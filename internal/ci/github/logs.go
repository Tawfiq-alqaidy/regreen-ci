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

// FailureLog returns the log lines of the failed step named by id, without
// GitHub's framing. A failure with no job (a run-level failure) has no log
// and yields "". The download URL is a signed secret: it never appears in an
// error, and the API token is never sent to it.
func (p *Provider) FailureLog(ctx context.Context, repo ci.Repo, id string) (string, error) {
	if err := validateRepo(repo); err != nil {
		return "", err
	}
	runID, jobID, step, err := parseID(id)
	if err != nil {
		return "", err
	}
	if jobID == 0 {
		return "", nil
	}

	job, _, err := p.client.Actions.GetWorkflowJobByID(ctx, repo.Owner, repo.Name, jobID)
	if err != nil {
		return "", fmt.Errorf("get job %d of %s: %w", jobID, repo, err)
	}
	if job.GetRunID() != runID {
		return "", errors.New("failure id does not match job")
	}
	var start, end time.Time
	if step != 0 {
		for _, s := range job.Steps {
			if s.GetNumber() == int64(step) {
				start, end = s.GetStartedAt().Time, s.GetCompletedAt().Time
				break
			}
		}
	}

	raw, err := p.downloadLog(ctx, repo, jobID)
	if err != nil {
		return "", err
	}
	return stripFraming(stepSection(raw, start, end)), nil
}

// downloadLog fetches the job log, keeping at most maxLogBytes of its tail.
func (p *Provider) downloadLog(ctx context.Context, repo ci.Repo, jobID int64) (string, error) {
	// The URL is a signed secret, so every error below says what failed and
	// at most a status or a fixed cause; none wraps an error whose text may
	// carry the URL.
	logURL, resp, err := p.client.Actions.GetWorkflowJobLogs(ctx, repo.Owner, repo.Name, jobID, maxDownloadRedirects)
	if err != nil {
		if cause := ctxCause(err); cause != nil {
			return "", fmt.Errorf("request log location of job %d: %w", jobID, cause)
		}
		return "", fmt.Errorf("request log location of job %d of %s: %s", jobID, repo, apiFailure(err, resp))
	}
	if logURL == nil || logURL.String() == "" {
		return "", fmt.Errorf("log location of job %d: %w", jobID, errLogURLMissing)
	}
	base, err := url.Parse(p.client.BaseURL())
	if err != nil {
		return "", fmt.Errorf("parse API base URL: %w", err)
	}
	logURL = base.ResolveReference(logURL)
	allowHTTP := p.baseCustom && base.Scheme == "http"
	if err := checkLogURL(logURL, allowHTTP); err != nil {
		return "", fmt.Errorf("log location of job %d: %w", jobID, err)
	}

	// A client of its own, with no credentials: the log is on another host
	// and the URL is already signed.
	client := &http.Client{
		Timeout: httpTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxDownloadRedirects {
				return errTooManyRedirects
			}
			return checkLogURL(req.URL, allowHTTP)
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, logURL.String(), http.NoBody)
	if err != nil {
		return "", fmt.Errorf("download log of job %d failed", jobID)
	}
	req.Header.Set("User-Agent", userAgent)
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download log of job %d: %w", jobID, downloadCause(err))
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download log of job %d: HTTP status %d", jobID, res.StatusCode)
	}
	body, err := readTail(res.Body, maxLogBytes)
	if err != nil {
		return "", fmt.Errorf("read log of job %d: %w", jobID, err)
	}
	return string(body), nil
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
// line boundary when anything was dropped. It never holds much more than
// 2*limit bytes.
func readTail(r io.Reader, limit int) ([]byte, error) {
	hardCap := 2*limit + readChunk
	var buf []byte
	dropped := false
	for {
		if cap(buf)-len(buf) < readChunk {
			grown := make([]byte, len(buf), min(max(2*cap(buf), 2*readChunk), hardCap))
			copy(grown, buf)
			buf = grown
		}
		n, err := r.Read(buf[len(buf) : len(buf)+readChunk])
		buf = buf[:len(buf)+n]
		if len(buf) >= 2*limit {
			buf = buf[:copy(buf, buf[len(buf)-limit:])]
			dropped = true
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
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
	return buf, nil
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

// stepSection returns the lines of raw that belong to a step that ran from
// start to end. The API reports times truncated to the second, so lines up to
// one second past end are kept. A line without a timestamp belongs with the
// line before it. When the window is unusable or selects nothing, the whole
// log is returned: a longer excerpt is better than an empty one.
func stepSection(raw string, start, end time.Time) string {
	raw = strings.TrimPrefix(raw, byteOrderMark)
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return raw
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
		return raw
	}
	return kept.String()
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
