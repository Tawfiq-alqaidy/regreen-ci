package github

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Tawfiq-alqaidy/regreen-ci/internal/ci"
	gogithub "github.com/google/go-github/v93/github"
)

const (
	byteOrderMark = "\xef\xbb\xbf"

	// maxLogBytes is how much of a job log is kept. When a log is larger the
	// tail is kept: a failure is reported at the end of its step.
	maxLogBytes = 8 << 20

	maxDownloadRedirects = 2
	readChunk            = 32 << 10
)

// Causes the download may report. Their texts are fixed: nothing taken from
// the signed URL or from a server's response ever reaches an error.
var (
	errLogURLMissing    = errors.New("log download URL is missing")
	errLogURLScheme     = errors.New("log download URL scheme is not allowed")
	errTooManyRedirects = errors.New("too many redirects")
)

// errNotFailedStep rejects an ID that parses but does not name a failed step
// of its job.
var errNotFailedStep = errors.New("failure id does not name a failed step of this job")

// FailureLog resolves id to the failed step it names and returns that step's
// log lines without GitHub's framing. The ID is caller-supplied, so Step and
// Commit are rebuilt from the job GitHub reports, and an ID that does not
// name a failed step of a completed, failed job is an error. A step-0 ID is
// valid only for a failed job without a failed step; its log is the whole
// job log.
//
// A run-level ID (job 0) has no log and no commit binding: FailureLog
// returns its canonical ID only, without any request. Callers get the commit
// of a run-level failure from CommitStatus.
//
// Partial is set when the log was cut to its last maxLogBytes, and when the
// step's time window selects no line or is unusable; then Log is empty, so
// other steps' output never stands in for the step's. The download URL is a
// signed secret: it never appears in an error, and the API token is never
// sent to it.
func (p *Provider) FailureLog(ctx context.Context, repo ci.Repo, id string) (ci.StepLog, error) {
	if err := validateRepo(repo); err != nil {
		return ci.StepLog{}, err
	}
	runID, jobID, stepNumber, err := parseID(id)
	if err != nil {
		return ci.StepLog{}, err
	}
	canonical := encodeID(runID, jobID, stepNumber)
	if jobID == 0 {
		return ci.StepLog{Step: ci.FailedStep{ID: canonical}}, nil
	}

	job, _, err := p.client.Actions.GetWorkflowJobByID(ctx, repo.Owner, repo.Name, jobID)
	if err != nil {
		return ci.StepLog{}, fmt.Errorf("get job %d of %s: %w", jobID, repo, err)
	}
	if job.GetRunID() != runID {
		return ci.StepLog{}, errors.New("failure id does not match job")
	}
	step, err := failedStepOf(job, stepNumber)
	if err != nil {
		return ci.StepLog{}, err
	}
	commit := job.GetHeadSHA()
	if !ci.ValidCommit(commit) {
		return ci.StepLog{}, fmt.Errorf("job %d has no valid head commit", jobID)
	}

	raw, cut, err := p.downloadLog(ctx, repo, jobID)
	if err != nil {
		return ci.StepLog{}, err
	}
	sl := ci.StepLog{
		Step: ci.FailedStep{
			ID:         canonical,
			Workflow:   job.GetWorkflowName(),
			Job:        job.GetName(),
			StepNumber: stepNumber,
			URL:        job.GetHTMLURL(),
		},
		Commit:  commit,
		Partial: cut,
	}
	if step == nil {
		// The job failed without a failed step: the whole log is the
		// step's best available text.
		sl.Log = stripFraming(strings.TrimPrefix(raw, byteOrderMark))
		return sl, nil
	}
	sl.Step.Step = step.GetName()
	start, end := step.GetStartedAt().Time, step.GetCompletedAt().Time
	if !usableWindow(start, end) {
		sl.Partial = true
		return sl, nil
	}
	section, matched := stepSection(raw, start, end)
	if !matched {
		sl.Partial = true
		return sl, nil
	}
	sl.Log = stripFraming(section)
	return sl, nil
}

// failedStepOf checks that job is a completed, failed job and that n names
// one of its failed steps. It returns that step, or nil for n == 0, which is
// valid only when the job has no failed step.
func failedStepOf(job *gogithub.WorkflowJob, n int) (*gogithub.TaskStep, error) {
	if job.GetStatus() != "completed" || passes(job.GetConclusion()) {
		return nil, errNotFailedStep
	}
	var named *gogithub.TaskStep
	anyFailed := false
	for _, s := range job.Steps {
		if s.GetConclusion() != "failure" {
			continue
		}
		anyFailed = true
		if n != 0 && s.GetNumber() == int64(n) {
			named = s
		}
	}
	switch {
	case n == 0 && anyFailed, n != 0 && named == nil:
		return nil, errNotFailedStep
	}
	return named, nil
}

// downloadLog fetches the job log, keeping at most maxLogBytes of its tail.
// cut reports that the start of the log was dropped.
func (p *Provider) downloadLog(ctx context.Context, repo ci.Repo, jobID int64) (log string, cut bool, err error) {
	// The URL is a signed secret, so every error below says what failed and
	// at most a status or a fixed cause; none wraps an error whose text may
	// carry the URL.
	logURL, resp, err := p.client.Actions.GetWorkflowJobLogs(ctx, repo.Owner, repo.Name, jobID, maxDownloadRedirects)
	if err != nil {
		if cause := ctxCause(err); cause != nil {
			return "", false, fmt.Errorf("request log location of job %d: %w", jobID, cause)
		}
		return "", false, fmt.Errorf("request log location of job %d of %s: %s", jobID, repo, apiFailure(err, resp))
	}
	// GitHub answers with an absolute URL on another host. Anything else,
	// a relative reference included, is refused rather than resolved
	// against the API base, where it would be fetched as a log.
	if logURL == nil || !logURL.IsAbs() || logURL.Host == "" {
		return "", false, fmt.Errorf("log location of job %d: %w", jobID, errLogURLMissing)
	}
	if err := checkLogURL(logURL, p.allowHTTP); err != nil {
		return "", false, fmt.Errorf("log location of job %d: %w", jobID, err)
	}

	// A client of its own, with no credentials: the log is on another host
	// and the URL is already signed.
	client := &http.Client{
		Timeout: httpTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxDownloadRedirects {
				return errTooManyRedirects
			}
			return checkLogURL(req.URL, p.allowHTTP)
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, logURL.String(), http.NoBody)
	if err != nil {
		return "", false, fmt.Errorf("download log of job %d failed", jobID)
	}
	req.Header.Set("User-Agent", userAgent)
	res, err := client.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("download log of job %d: %w", jobID, downloadCause(err))
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("download log of job %d: HTTP status %d", jobID, res.StatusCode)
	}
	body, cut, err := readTail(res.Body, maxLogBytes)
	if err != nil {
		return "", false, fmt.Errorf("read log of job %d: %w", jobID, err)
	}
	return string(body), cut, nil
}

// ctxCause returns the context error err stems from, or nil.
func ctxCause(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	}
	return nil
}

// downloadCause reduces a failed download to an error with a fixed text. The
// *url.Error client.Do returns carries the signed URL, and a cause from the
// net/http machinery can quote a Location header, so only the causes listed
// here pass through.
func downloadCause(err error) error {
	if cause := ctxCause(err); cause != nil {
		return cause
	}
	for _, known := range []error{errTooManyRedirects, errLogURLScheme, errLogURLMissing} {
		if errors.Is(err, known) {
			return known
		}
	}
	if ne, ok := errors.AsType[net.Error](err); ok && ne.Timeout() {
		return errors.New("download timed out")
	}
	return errors.New("download failed")
}

// apiFailure describes a failed log-location request without quoting err,
// which may contain the redirect target.
func apiFailure(err error, resp *gogithub.Response) string {
	if e, ok := errors.AsType[*gogithub.ErrorResponse](err); ok && e.Response != nil {
		return fmt.Sprintf("HTTP status %d", e.Response.StatusCode)
	}
	if e, ok := errors.AsType[*gogithub.RateLimitError](err); ok && e.Response != nil {
		return fmt.Sprintf("HTTP status %d", e.Response.StatusCode)
	}
	if e, ok := errors.AsType[*gogithub.AbuseRateLimitError](err); ok && e.Response != nil {
		return fmt.Sprintf("HTTP status %d", e.Response.StatusCode)
	}
	if resp != nil && resp.Response != nil {
		return fmt.Sprintf("HTTP status %d", resp.StatusCode)
	}
	if ne, ok := errors.AsType[net.Error](err); ok {
		if ne.Timeout() {
			return "network error: timeout"
		}
		return "network error"
	}
	return "request failed"
}

// checkLogURL accepts only https URLs, and http when allowHTTP is set. Its
// errors never include the URL.
func checkLogURL(u *url.URL, allowHTTP bool) error {
	switch {
	case u.Host == "":
		return errLogURLMissing
	case u.Scheme == "https", u.Scheme == "http" && allowHTTP:
		return nil
	}
	return errLogURLScheme
}

// readTail reads r to the end and returns its last limit bytes, starting at a
// line boundary when anything was dropped; dropped reports that. It never
// holds much more than 2*limit bytes.
func readTail(r io.Reader, limit int) (tail []byte, dropped bool, err error) {
	hardCap := 2*limit + readChunk
	var buf []byte
	for {
		if cap(buf)-len(buf) < readChunk {
			grown := make([]byte, len(buf), min(max(2*cap(buf), 2*readChunk), hardCap))
			copy(grown, buf)
			buf = grown
		}
		n, rerr := r.Read(buf[len(buf) : len(buf)+readChunk])
		buf = buf[:len(buf)+n]
		if len(buf) >= 2*limit {
			buf = buf[:copy(buf, buf[len(buf)-limit:])]
			dropped = true
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			return nil, false, rerr
		}
	}
	if len(buf) > limit {
		buf = buf[len(buf)-limit:]
		dropped = true
	}
	if dropped {
		// The first line is probably cut. With no newline at all, keep the
		// tail: a partial line beats nothing.
		if i := bytes.IndexByte(buf, '\n'); i >= 0 {
			buf = buf[i+1:]
		}
	}
	return buf, dropped, nil
}

// splitTimestamp returns the line's leading RFC 3339 timestamp and the text
// after it (without the one separating space). ok is false when the first
// space-delimited field is not a timestamp.
func splitTimestamp(line string) (t time.Time, rest string, ok bool) {
	field, rest, _ := strings.Cut(line, " ")
	t, err := time.Parse(time.RFC3339Nano, field)
	if err != nil {
		return time.Time{}, "", false
	}
	return t, rest, true
}

// lineBody is line without its line ending, "\n" or "\r\n".
func lineBody(line string) string {
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
}

// usableWindow reports whether start and end describe a step's run time.
func usableWindow(start, end time.Time) bool {
	return !start.IsZero() && !end.IsZero() && !end.Before(start)
}

// stepSection returns the lines of raw that belong to a step that ran from
// start to end, and whether the window selected any line. The API reports
// times truncated to the second, so lines up to one second past end are
// kept. A line without a timestamp belongs with the line before it. When the
// window is unusable or selects nothing, the whole log is returned with
// matched false; FailureLog tells the two apart with usableWindow and does
// not use the whole log for a numbered step.
func stepSection(raw string, start, end time.Time) (section string, matched bool) {
	raw = strings.TrimPrefix(raw, byteOrderMark)
	if !usableWindow(start, end) {
		return raw, false
	}
	limit := end.Add(time.Second)
	var kept strings.Builder
	keep := false
	for _, line := range strings.SplitAfter(raw, "\n") {
		if t, _, ok := splitTimestamp(lineBody(line)); ok {
			keep = !t.Before(start) && t.Before(limit)
		}
		if keep {
			kept.WriteString(line)
		}
	}
	if kept.Len() == 0 {
		return raw, false
	}
	return kept.String(), true
}

// stripFraming removes GitHub's per-line timestamps and workflow command
// markers, and nothing else. Sanitizing and capping is failure.TrimLog's job.
func stripFraming(section string) string {
	var out strings.Builder
	out.Grow(len(section))
	for _, piece := range strings.SplitAfter(section, "\n") {
		body := lineBody(piece)
		ending := piece[len(body):] // "", "\n" or "\r\n", kept as it was
		if _, rest, ok := splitTimestamp(body); ok {
			body = rest
		}
		if body == "##[endgroup]" {
			continue
		}
		out.WriteString(stripMarker(body))
		out.WriteString(ending)
	}
	return out.String()
}

var markerReplacements = []struct{ prefix, with string }{
	{"##[group]", ""},
	{"##[command]", ""},
	{"[command]", ""},
	{"##[debug]", ""},
	{"##[error]", "error: "},
	{"##[warning]", "warning: "},
	{"##[notice]", "notice: "},
}

func stripMarker(line string) string {
	for _, m := range markerReplacements {
		if rest, ok := strings.CutPrefix(line, m.prefix); ok {
			return m.with + rest
		}
	}
	return line
}
