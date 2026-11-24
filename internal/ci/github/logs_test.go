package github

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Tawfiq-alqaidy/regreen-ci/internal/ci"
	"github.com/Tawfiq-alqaidy/regreen-ci/internal/config"
	"github.com/Tawfiq-alqaidy/regreen-ci/internal/failure"
)

const bom = "\xef\xbb\xbf"

var (
	step3Start = time.Date(2026, 11, 24, 10, 0, 4, 0, time.UTC)
	step3End   = time.Date(2026, 11, 24, 10, 0, 9, 0, time.UTC)
)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	root, err := os.OpenRoot("testdata")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	b, err := root.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestFixturesKeepTheirBOM makes sure an editor did not silently strip the
// byte order mark the real logs start with.
func TestFixturesKeepTheirBOM(t *testing.T) {
	for _, name := range []string{"job_lint_failed.log", "job_no_timestamps.log"} {
		t.Run(name, func(t *testing.T) {
			raw := readFixture(t, name)
			if !strings.HasPrefix(raw, bom) {
				t.Fatalf("%s lost its UTF-8 BOM", name)
			}
			if n := strings.Count(raw, "\n"); n >= 60 {
				t.Fatalf("%s has %d lines, want fewer than 60", name, n)
			}
		})
	}
}

func TestStepSection(t *testing.T) {
	lint := readFixture(t, "job_lint_failed.log")
	plain := readFixture(t, "job_no_timestamps.log")
	wholeLint := strings.TrimPrefix(lint, bom)
	wholePlain := strings.TrimPrefix(plain, bom)
	t0 := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		raw        string
		start, end time.Time
		want       string
		matched    bool
	}{
		{"zero start", lint, time.Time{}, step3End, wholeLint, false},
		{"zero end", lint, step3Start, time.Time{}, wholeLint, false},
		{"end before start", lint, step3End, step3Start, wholeLint, false},
		{"no timestamps", plain, step3Start, step3End, wholePlain, false},
		{"window matches nothing", lint, t0.Add(time.Hour), t0.Add(2 * time.Hour), wholeLint, false},
		{"BOM removed", bom + "2026-11-24T10:00:04.0000000Z a\n", step3Start, step3End, "2026-11-24T10:00:04.0000000Z a\n", true},
		{"one BOM only", bom + bom + "x\n", time.Time{}, time.Time{}, bom + "x\n", false},
		{
			"continuation lines stay with their line",
			"2026-11-24T10:00:03.0000000Z before\n" +
				"2026-11-24T10:00:05.0000000Z first\nsecond\n  third\n" +
				"2026-11-24T10:00:06.0000000Z next\n" +
				"2026-11-24T10:00:10.0000000Z after\ntail of after\n",
			step3Start, step3End,
			"2026-11-24T10:00:05.0000000Z first\nsecond\n  third\n2026-11-24T10:00:06.0000000Z next\n",
			true,
		},
		{
			"untimestamped lines before the first timestamp are dropped",
			"orphan\n2026-11-24T10:00:05.0000000Z kept\n",
			step3Start, step3End,
			"2026-11-24T10:00:05.0000000Z kept\n",
			true,
		},
		{
			"start is inclusive, end plus one second exclusive",
			"2026-11-24T10:00:03.9999999Z a\n2026-11-24T10:00:04.0000000Z b\n2026-11-24T10:00:09.9999999Z c\n2026-11-24T10:00:10.0000000Z d\n",
			step3Start, step3End,
			"2026-11-24T10:00:04.0000000Z b\n2026-11-24T10:00:09.9999999Z c\n",
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, matched := stepSection(tt.raw, tt.start, tt.end)
			if got != tt.want || matched != tt.matched {
				t.Errorf("stepSection =\n%q, %v\nwant\n%q, %v", got, matched, tt.want, tt.matched)
			}
		})
	}
}

func TestStepSectionSelectsStepThreeOfTheFixture(t *testing.T) {
	got, matched := stepSection(readFixture(t, "job_lint_failed.log"), step3Start, step3End)
	if !matched {
		t.Fatal("window selected nothing")
	}
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if first := lines[0]; !strings.HasSuffix(first, "##[group]Run npm run lint") {
		t.Errorf("first line = %q", first)
	}
	if last := lines[len(lines)-1]; !strings.Contains(last, "10:00:09.9000000Z") || !strings.Contains(last, "##[error]Process completed") {
		t.Errorf("last line = %q, want the 10:00:09.9 line", last)
	}
	for _, bad := range []string{"src/secret.ts", "Post job cleanup", "Set up job", bom} {
		if strings.Contains(got, bad) {
			t.Errorf("section contains %q", bad)
		}
	}
	if !strings.Contains(got, "src/a.ts") {
		t.Error("section misses src/a.ts")
	}
}

func TestStripFraming(t *testing.T) {
	const ts = "2026-11-24T10:00:04.1234567Z "
	tests := []struct {
		name, in, want string
	}{
		{"timestamp removed", ts + "hello world\n", "hello world\n"},
		{"only one space after the timestamp", ts + "  indented\n", "  indented\n"},
		{"timestamp alone", "2026-11-24T10:00:04.1234567Z\nx\n", "\nx\n"},
		{"endgroup dropped", ts + "a\n" + ts + "##[endgroup]\n" + ts + "b\n", "a\nb\n"},
		{"endgroup without timestamp dropped", "a\n##[endgroup]\nb", "a\nb"},
		{"group", ts + "##[group]Run make\n", "Run make\n"},
		{"command marker", ts + "##[command]git status\n", "git status\n"},
		{"bare command marker", ts + "[command]/usr/bin/git log\n", "/usr/bin/git log\n"},
		{"debug", ts + "##[debug]Evaluating\n", "Evaluating\n"},
		{"error", ts + "##[error]Process completed with exit code 1.\n", "error: Process completed with exit code 1.\n"},
		{"warning", ts + "##[warning]careful\n", "warning: careful\n"},
		{"notice", ts + "##[notice]fyi\n", "notice: fyi\n"},
		{"marker in the middle untouched", ts + "see ##[error] in the middle\n", "see ##[error] in the middle\n"},
		{"endgroup text in the middle untouched", ts + "x ##[endgroup]\n", "x ##[endgroup]\n"},
		{"line without timestamp keeps its text", "plain ##[group]x\n", "plain ##[group]x\n"},
		{"non timestamp first field kept", "not-a-time text\n", "not-a-time text\n"},
		{"ANSI untouched", ts + "\x1b[31mred\x1b[0m\n", "\x1b[31mred\x1b[0m\n"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripFraming(tt.in); got != tt.want {
				t.Errorf("stripFraming(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// --- FailureLog through the fake API ---

const (
	lintRun int64 = 500
	lintJob int64 = 7001
)

func lintFixtureJob() fakeJob {
	return fakeJob{
		ID: lintJob, RunID: lintRun, Name: "lint", Status: "completed", Conclusion: "failure",
		HTMLURL: "https://example.test/job/7001", HeadSHA: testSHA, WorkflowName: "CI",
		Steps: []fakeStep{
			{Name: "Set up job", Number: 1, Status: "completed", Conclusion: "success", StartedAt: "2026-11-24T10:00:00Z", CompletedAt: "2026-11-24T10:00:02Z"},
			{Name: "Run actions/checkout@v4", Number: 2, Status: "completed", Conclusion: "success", StartedAt: "2026-11-24T10:00:02Z", CompletedAt: "2026-11-24T10:00:04Z"},
			{Name: "Run npm run lint", Number: 3, Status: "completed", Conclusion: "failure", StartedAt: "2026-11-24T10:00:04Z", CompletedAt: "2026-11-24T10:00:09Z"},
			{Name: "Complete job", Number: 4, Status: "completed", Conclusion: "success", StartedAt: "2026-11-24T10:00:10Z", CompletedAt: "2026-11-24T10:00:11Z"},
		},
	}
}

func lintFake(t *testing.T) (*fakeGitHub, *Provider) {
	t.Helper()
	f := newFakeGitHub(t)
	f.jobByID[lintJob] = lintFixtureJob()
	f.logs[lintJob] = []byte(readFixture(t, "job_lint_failed.log"))
	return f, newTestProvider(t, f)
}

// cancelledJob is lintFixtureJob after a cancellation: it failed without a
// failed step, so only its step-0 ID is valid.
func cancelledJob() fakeJob {
	j := lintFixtureJob()
	j.Conclusion = "cancelled"
	j.Steps[2].Conclusion = "cancelled"
	return j
}

func failureLog(t *testing.T, p *Provider, id string) (ci.StepLog, error) {
	t.Helper()
	got, err := p.FailureLog(context.Background(), testRepo, id)
	noLeak(t, err)
	if err != nil {
		noSignedURL(t, err)
	}
	return got, err
}

// noSignedURL fails when err mentions the download signature or path.
func noSignedURL(t *testing.T, err error) {
	t.Helper()
	for _, bad := range []string{"SECRETSIG", "sig=", "/download/"} {
		if strings.Contains(err.Error(), bad) {
			t.Fatalf("error leaks the signed download URL (%q): %v", bad, err)
		}
	}
}

func TestFailureLogReturnsTheFailedStep(t *testing.T) {
	f, p := lintFake(t)
	sl, err := failureLog(t, p, encodeID(lintRun, lintJob, 3))
	if err != nil {
		t.Fatal(err)
	}
	wantStep := ci.FailedStep{
		ID: "gha-500-7001-3", Workflow: "CI", Job: "lint", Step: "Run npm run lint", StepNumber: 3,
		URL: "https://example.test/job/7001",
	}
	if sl.Step != wantStep || sl.Commit != testSHA || sl.Partial {
		t.Errorf("Step = %+v, Commit = %q, Partial = %v; want %+v, %q, false", sl.Step, sl.Commit, sl.Partial, wantStep, testSHA)
	}
	got := sl.Log
	if !strings.HasPrefix(got, "Run npm run lint\n") {
		t.Errorf("log does not start with the step's group line:\n%s", got)
	}
	for _, want := range []string{
		"src/a.ts", "12:5  error  'x' is assigned a value but never used",
		"error: Process completed with exit code 1.", "warning: 1 problem found", "shell: /usr/bin/bash -e {0}",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("log misses %q:\n%s", want, got)
		}
	}
	for _, bad := range []string{"src/secret.ts", "Post job cleanup", "Set up job", "##[", "2026-11-24T", bom} {
		if strings.Contains(got, bad) {
			t.Errorf("log contains %q:\n%s", bad, got)
		}
	}

	// The token goes to the API only, never to the download host.
	var api, download int
	for _, r := range f.allRequests() {
		if strings.HasPrefix(r.Path, "/download/") {
			download++
			if r.Auth != "" {
				t.Errorf("download request carried Authorization %q", r.Auth)
			}
			if r.Query.Get("sig") != "SECRETSIG" {
				t.Errorf("download request lost its signature: %v", r.Query)
			}
			continue
		}
		api++
		if r.Auth != "Bearer "+testToken {
			t.Errorf("API request %s Authorization = %q", r.Path, r.Auth)
		}
	}
	if download != 1 || api != 2 {
		t.Errorf("requests: %d API, %d download; want 2 and 1", api, download)
	}
}

func TestFailureLogStepZeroIsTheWholeLog(t *testing.T) {
	f, p := lintFake(t)
	f.jobByID[lintJob] = cancelledJob()
	sl, err := failureLog(t, p, encodeID(lintRun, lintJob, 0))
	if err != nil {
		t.Fatal(err)
	}
	if sl.Partial || sl.Commit != testSHA || sl.Step.Step != "" || sl.Step.StepNumber != 0 || sl.Step.ID != "gha-500-7001-0" {
		t.Errorf("unexpected StepLog fields: %+v", sl)
	}
	for _, want := range []string{"Set up job", "src/secret.ts", "src/a.ts", "Post job cleanup", "error: Process completed"} {
		if !strings.Contains(sl.Log, want) {
			t.Errorf("whole log misses %q", want)
		}
	}
	for _, bad := range []string{"##[", "2026-11-24T", bom} {
		if strings.Contains(sl.Log, bad) {
			t.Errorf("log still has framing %q", bad)
		}
	}
}

func TestFailureLogCanonicalID(t *testing.T) {
	_, p := lintFake(t)
	sl, err := failureLog(t, p, "gha-0500-007001-03")
	if err != nil {
		t.Fatal(err)
	}
	if sl.Step.ID != "gha-500-7001-3" {
		t.Errorf("ID = %q, want the canonical gha-500-7001-3", sl.Step.ID)
	}
}

// TestFailureLogEmptyWindowIsPartial covers a numbered step whose time
// window cannot select its lines: the log is empty and marked Partial, so
// other steps' output never reaches Failure.Files.
func TestFailureLogEmptyWindowIsPartial(t *testing.T) {
	tests := []struct {
		name  string
		setup func(f *fakeGitHub)
	}{
		{"window selects no line", func(f *fakeGitHub) {
			j := lintFixtureJob()
			j.Steps[2].StartedAt, j.Steps[2].CompletedAt = "2026-11-24T11:00:00Z", "2026-11-24T11:00:05Z"
			f.jobByID[lintJob] = j
		}},
		{"log has no timestamps", func(f *fakeGitHub) {
			f.logs[lintJob] = []byte(readFixture(f.t, "job_no_timestamps.log"))
		}},
		{"step times missing", func(f *fakeGitHub) {
			j := lintFixtureJob()
			j.Steps[2].StartedAt, j.Steps[2].CompletedAt = "", ""
			f.jobByID[lintJob] = j
		}},
		{"step ends before it starts", func(f *fakeGitHub) {
			j := lintFixtureJob()
			j.Steps[2].StartedAt, j.Steps[2].CompletedAt = "2026-11-24T10:00:09Z", "2026-11-24T10:00:04Z"
			f.jobByID[lintJob] = j
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, p := lintFake(t)
			tt.setup(f)
			sl, err := failureLog(t, p, encodeID(lintRun, lintJob, 3))
			if err != nil {
				t.Fatal(err)
			}
			if sl.Log != "" || !sl.Partial {
				t.Errorf("Log = %q, Partial = %v; want empty and Partial", sl.Log, sl.Partial)
			}
			if sl.Commit != testSHA || sl.Step.Step != "Run npm run lint" {
				t.Errorf("step binding lost: %+v", sl)
			}
		})
	}
}

func TestFailureLogRunLevelFailureHasNoLog(t *testing.T) {
	f, p := lintFake(t)
	got, err := failureLog(t, p, "gha-0500-0-0")
	want := ci.StepLog{Step: ci.FailedStep{ID: "gha-500-0-0"}}
	if err != nil || got != want {
		t.Fatalf("got %+v, %v; want %+v, nil", got, err, want)
	}
	if n := len(f.allRequests()); n != 0 {
		t.Errorf("%d requests made for a run-level failure", n)
	}
}

// TestFailureLogRejectsIDsThatNameNoFailedStep covers IDs that parse but do
// not name a failed step: they fail before any log is downloaded.
func TestFailureLogRejectsIDsThatNameNoFailedStep(t *testing.T) {
	withJob := func(edit func(j *fakeJob)) func(f *fakeGitHub) {
		return func(f *fakeGitHub) {
			j := lintFixtureJob()
			edit(&j)
			f.jobByID[lintJob] = j
		}
	}
	tests := []struct {
		name  string
		step  int
		setup func(f *fakeGitHub)
		want  string
	}{
		{"passing step", 2, nil, errNotFailedStep.Error()},
		{"step the job does not have", 99, nil, errNotFailedStep.Error()},
		{"step 0 while a step failed", 0, nil, errNotFailedStep.Error()},
		{"job in progress", 3, withJob(func(j *fakeJob) { j.Status, j.Conclusion = "in_progress", "" }), errNotFailedStep.Error()},
		{"job queued", 3, withJob(func(j *fakeJob) { j.Status, j.Conclusion = "queued", "" }), errNotFailedStep.Error()},
		{"job succeeded", 3, withJob(func(j *fakeJob) { j.Conclusion = "success" }), errNotFailedStep.Error()},
		{"job skipped", 3, withJob(func(j *fakeJob) { j.Conclusion = "skipped" }), errNotFailedStep.Error()},
		{"job neutral", 3, withJob(func(j *fakeJob) { j.Conclusion = "neutral" }), errNotFailedStep.Error()},
		{"step 0 of a passing job", 0, withJob(func(j *fakeJob) {
			j.Conclusion = "success"
			j.Steps[2].Conclusion = "success"
		}), errNotFailedStep.Error()},
		{"step cancelled, not failed", 3, func(f *fakeGitHub) { f.jobByID[lintJob] = cancelledJob() }, errNotFailedStep.Error()},
		{"head commit missing", 3, withJob(func(j *fakeJob) { j.HeadSHA = "" }), "no valid head commit"},
		{"head commit abbreviated", 3, withJob(func(j *fakeJob) { j.HeadSHA = testSHA[:12] }), "no valid head commit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, p := lintFake(t)
			if tt.setup != nil {
				tt.setup(f)
			}
			sl, err := failureLog(t, p, encodeID(lintRun, lintJob, tt.step))
			if err == nil || err.Error() != tt.want && !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %+v, %v; want error %q", sl, err, tt.want)
			}
			if sl != (ci.StepLog{}) {
				t.Errorf("returned %+v alongside an error", sl)
			}
			if n := f.requestCount("/logs") + f.requestCount(fmt.Sprintf("/download/%d", lintJob)); n != 0 {
				t.Errorf("%d log requests made for a rejected id", n)
			}
		})
	}
}

// TestFailureLogMatchesCommitStatus resolves an ID CommitStatus issued, then
// the same job with a step number that did not fail.
func TestFailureLogMatchesCommitStatus(t *testing.T) {
	f, p := lintFake(t)
	r := failedRun(lintRun)
	r.Name = "CI"
	f.runs = []fakeRun{r}
	f.jobs[lintRun] = []fakeJob{lintFixtureJob()}

	st := status(t, p)
	if len(st.Failed) != 1 {
		t.Fatalf("Failed = %+v", st.Failed)
	}
	issued := st.Failed[0]
	sl, err := failureLog(t, p, issued.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sl.Step != issued || sl.Commit != st.Commit {
		t.Errorf("resolved %+v on %q, CommitStatus issued %+v on %q", sl.Step, sl.Commit, issued, st.Commit)
	}

	forged := encodeID(lintRun, lintJob, 2)
	if _, err := failureLog(t, p, forged); err == nil || err.Error() != errNotFailedStep.Error() {
		t.Fatalf("forged id %s: err = %v, want %q", forged, err, errNotFailedStep)
	}
}

func TestFailureLogErrors(t *testing.T) {
	tests := []struct {
		name  string
		id    string
		setup func(f *fakeGitHub)
		want  string
	}{
		{"malformed id", "nope", nil, "invalid failure id"},
		{"run id mismatch", encodeID(lintRun+1, lintJob, 3), nil, "does not match job"},
		{"unknown job", encodeID(lintRun, lintJob+1, 3), nil, "get job"},
		{"download 404", encodeID(lintRun, lintJob, 3), func(f *fakeGitHub) { f.downloadStatus[lintJob] = 404 }, "404"},
		{"download 500", encodeID(lintRun, lintJob, 3), func(f *fakeGitHub) { f.downloadStatus[lintJob] = 500 }, "500"},
		{"log request refused", encodeID(lintRun, lintJob, 3), func(f *fakeGitHub) {
			f.fail = func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/logs") {
					http.Error(w, "gone", http.StatusGone)
					return
				}
				writeJSON(w, lintFixtureJob())
			}
		}, "410"},
		{"redirect to a non-http scheme", encodeID(lintRun, lintJob, 3), func(f *fakeGitHub) { f.logLocation = "ftp://example.test/log?sig=SECRETSIG" }, "scheme"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, p := lintFake(t)
			if tt.setup != nil {
				tt.setup(f)
			}
			got, err := failureLog(t, p, tt.id)
			if err == nil {
				t.Fatalf("no error, got %+v", got)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
			if got != (ci.StepLog{}) {
				t.Errorf("returned %+v alongside an error", got)
			}
		})
	}
}

func TestFailureLogCancelledContext(t *testing.T) {
	_, p := lintFake(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.FailureLog(ctx, testRepo, encodeID(lintRun, lintJob, 3))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	noLeak(t, err)
}

func TestFailureLogCancelledDuringDownload(t *testing.T) {
	f, p := lintFake(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.mu.Lock()
	f.logLocation = f.srv.URL + "/slow?sig=SECRETSIG"
	f.mu.Unlock()
	go func() {
		<-f.slowHit
		cancel()
	}()
	_, err := p.FailureLog(ctx, testRepo, encodeID(lintRun, lintJob, 3))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	noSignedURL(t, err)
}

func TestFailureLogKeepsTheTailOfAHugeLog(t *testing.T) {
	f, p := lintFake(t)
	var b strings.Builder
	for i := 0; b.Len() < 9<<20; i++ {
		fmt.Fprintf(&b, "line %07d %s\n", i, strings.Repeat("x", 80))
	}
	body := b.String()
	last := body[strings.LastIndex(strings.TrimSuffix(body, "\n"), "\n")+1:]
	f.logs[lintJob] = []byte(body)
	f.jobByID[lintJob] = cancelledJob()

	sl, err := failureLog(t, p, encodeID(lintRun, lintJob, 0))
	if err != nil {
		t.Fatal(err)
	}
	if !sl.Partial {
		t.Error("a log cut to its tail must be Partial")
	}
	got := sl.Log
	if len(got) > maxLogBytes || len(got) < maxLogBytes-200 {
		t.Errorf("len = %d, want just under %d", len(got), maxLogBytes)
	}
	if !strings.HasSuffix(got, last) {
		t.Error("result does not end with the body's last line")
	}
	if !strings.HasPrefix(got, "line ") {
		t.Errorf("result starts with a partial line: %.40q", got)
	}
}

func TestReadTail(t *testing.T) {
	lines := func(n int) string {
		var b strings.Builder
		for i := range n {
			fmt.Fprintf(&b, "l%02d\n", i) // 4 bytes each
		}
		return b.String()
	}
	tests := []struct {
		name    string
		in      string
		max     int
		want    string
		dropped bool
	}{
		{"under the cap", lines(3), 100, lines(3), false},
		{"exactly the cap", lines(5), 20, lines(5), false},
		{"over the cap drops the partial first line", lines(10), 14, "l07\nl08\nl09\n", true},
		{"many reads", lines(5000), 4000, lines(5000)[len(lines(5000))-3996:], true},
		{"empty", "", 10, "", false},
		{"no newline in the tail keeps the tail", strings.Repeat("a", 50), 10, strings.Repeat("a", 10), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, dropped, err := readTail(strings.NewReader(tt.in), tt.max)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want || dropped != tt.dropped {
				t.Errorf("readTail = %q, %v; want %q, %v", got, dropped, tt.want, tt.dropped)
			}
		})
	}
}

func TestCheckLogURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		allowHTTP bool
		wantErr   bool
	}{
		{"https", "https://pipelines.example/log?sig=x", false, false},
		{"http refused by default", "http://pipelines.example/log", false, true},
		{"http allowed for tests", "http://127.0.0.1:1234/log", true, false},
		{"ftp refused", "ftp://example/log", true, true},
		{"file refused", "file:///etc/passwd", true, true},
		{"no scheme", "//example/log", false, true},
		{"no host", "https:///log", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.url)
			if err != nil {
				t.Fatal(err)
			}
			err = checkLogURL(u, tt.allowHTTP)
			if (err != nil) != tt.wantErr {
				t.Fatalf("checkLogURL(%q, %v) = %v, wantErr %v", tt.url, tt.allowHTTP, err, tt.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), "sig=") {
				t.Errorf("error leaks the query: %v", err)
			}
		})
	}
}

func TestFailureLogFeedsFailureBuild(t *testing.T) {
	_, p := lintFake(t)
	sl, err := failureLog(t, p, encodeID(lintRun, lintJob, 3))
	if err != nil {
		t.Fatal(err)
	}
	checks := []config.Check{{Name: "lint", Run: "npm run lint"}}
	known := map[string]struct{}{"src/a.ts": {}, "src/secret.ts": {}}

	got := failure.Build(sl, checks, known, failure.DefaultLimits())
	if got.Commit != testSHA || got.ID != "gha-500-7001-3" || got.Job != "lint" || got.Truncated {
		t.Errorf("Build lost the binding: %+v", got)
	}
	if got.Check != "lint" || !got.Fixable {
		t.Errorf("Check = %q, Fixable = %v; want lint, true", got.Check, got.Fixable)
	}
	if want := []string{"src/a.ts"}; !reflect.DeepEqual(got.Files, want) {
		t.Errorf("Files = %v, want %v", got.Files, want)
	}
	if strings.Contains(got.Errors, "\x1b") {
		t.Errorf("Errors has an escape sequence: %q", got.Errors)
	}
	if strings.Contains(got.Errors, "2026-11-24T") {
		t.Errorf("Errors has a timestamp: %q", got.Errors)
	}
	if !strings.Contains(got.Errors, "no-unused-vars") {
		t.Errorf("Errors lost the lint message: %q", got.Errors)
	}
}

// --- redirect limits and error hygiene ---

// logsAt makes the fake's log request redirect to path on the fake server.
func logsAt(f *fakeGitHub, path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logLocation = f.srv.URL + path
}

func TestFailureLogDownloadRedirects(t *testing.T) {
	id := encodeID(lintRun, lintJob, 3)
	t.Run("two redirects succeed", func(t *testing.T) {
		f, p := lintFake(t)
		logsAt(f, fmt.Sprintf("/chain/2/%d?sig=SECRETSIG", lintJob))
		got, err := failureLog(t, p, id)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got.Log, "src/a.ts") {
			t.Errorf("log lost its content: %q", got.Log)
		}
	})
	t.Run("three redirects fail", func(t *testing.T) {
		f, p := lintFake(t)
		logsAt(f, fmt.Sprintf("/chain/3/%d?sig=SECRETSIG", lintJob))
		_, err := failureLog(t, p, id)
		if err == nil || !strings.Contains(err.Error(), "too many redirects") {
			t.Fatalf("err = %v, want too many redirects", err)
		}
		noSignedURL(t, err)
	})
	t.Run("hop to a non-http scheme is stopped by the per-hop check", func(t *testing.T) {
		f, p := lintFake(t)
		// The first URL passes the pre-request check; only the redirect
		// target is ftp, so the error can only come from CheckRedirect.
		logsAt(f, "/redirect?to="+url.QueryEscape("ftp://blob.example/log?sig=SECRETSIG"))
		_, err := failureLog(t, p, id)
		if err == nil || !errors.Is(err, errLogURLScheme) {
			t.Fatalf("err = %v, want the scheme error", err)
		}
		noSignedURL(t, err)
		if n := f.requestCount("/redirect"); n != 1 {
			t.Errorf("/redirect requested %d times, want 1 (the hop must be reached)", n)
		}
	})
	t.Run("malformed Location from the download host", func(t *testing.T) {
		f, p := lintFake(t)
		logsAt(f, "/redirect?to="+url.QueryEscape("http://blob.example/%zz?sig=SECOND_HOP_SECRET"))
		_, err := failureLog(t, p, id)
		if err == nil {
			t.Fatal("no error")
		}
		for _, bad := range []string{"SECOND_HOP_SECRET", "SECRETSIG", "blob.example", "%zz"} {
			if strings.Contains(err.Error(), bad) {
				t.Errorf("error contains %q: %v", bad, err)
			}
		}
		if !strings.Contains(err.Error(), "download failed") {
			t.Errorf("err = %v, want the fixed message", err)
		}
	})
}

// TestFailureLogMissingLocation covers a log location that is absent or not
// an absolute URL with a host. A relative one must not be resolved against
// the API base and fetched from there.
func TestFailureLogMissingLocation(t *testing.T) {
	tests := []struct {
		name  string
		setup func(f *fakeGitHub)
	}{
		{"no Location header", func(f *fakeGitHub) { f.noLocation = true }},
		{"empty Location", func(f *fakeGitHub) { f.emptyLocation = true }},
		{"relative API path", func(f *fakeGitHub) { f.logLocation = "repos/o/r/actions/jobs/7001" }},
		{"dot", func(f *fakeGitHub) { f.logLocation = "." }},
		{"slash", func(f *fakeGitHub) { f.logLocation = "/" }},
		{"scheme without host", func(f *fakeGitHub) { f.logLocation = "https:///download/7001" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, p := lintFake(t)
			tt.setup(f)
			got, err := failureLog(t, p, encodeID(lintRun, lintJob, 3))
			if err == nil || !strings.Contains(err.Error(), "log download URL is missing") {
				t.Fatalf("got %+v, %v; want the missing URL error", got, err)
			}
			var paths []string
			for _, r := range f.allRequests() {
				paths = append(paths, r.Path)
			}
			want := []string{"/repos/o/r/actions/jobs/7001", "/repos/o/r/actions/jobs/7001/logs"}
			if !reflect.DeepEqual(paths, want) {
				t.Errorf("requests = %q, want exactly %q", paths, want)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout http://x/?sig=SECRETSIG" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return false }

func TestFailureLogLocationRequestNetworkErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"connection refused", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}, "network error"},
		{"timeout", &net.OpError{Op: "read", Net: "tcp", Err: timeoutErr{}}, "timeout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := lintFake(t)
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, "/logs") {
					return nil, tt.err
				}
				return http.DefaultTransport.RoundTrip(r)
			})}
			p, err := New(testToken, WithBaseURL(f.baseURL()), WithHTTPClient(client))
			if err != nil {
				t.Fatal(err)
			}
			_, err = failureLog(t, p, encodeID(lintRun, lintJob, 3))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
			for _, bad := range []string{"dial", "tcp", "http://", "refused", "i/o"} {
				if strings.Contains(err.Error(), bad) {
					t.Errorf("error quotes the cause (%q): %v", bad, err)
				}
			}
		})
	}
}

func TestCRLFLogs(t *testing.T) {
	const a = "2026-11-24T10:00:04.0000000Z"
	const b = "2026-11-24T10:00:10.0000000Z"
	raw := bom + a + " ##[group]Run x\r\n" + a + " ##[endgroup]\r\n" + a + " ##[error]boom\r\n" +
		"2026-11-24T10:00:05.0000000Z\r\n" + b + " after\r\n"

	section, _ := stepSection(raw, step3Start, step3End)
	if strings.Contains(section, "after") {
		t.Errorf("window ignored on CRLF input: %q", section)
	}
	if !strings.Contains(section, "2026-11-24T10:00:05.0000000Z\r\n") {
		t.Errorf("timestamp-only CRLF line was not recognised as a timestamp line: %q", section)
	}
	got := stripFraming(section)
	want := "Run x\r\nerror: boom\r\n\r\n"
	if got != want {
		t.Errorf("stripFraming = %q, want %q", got, want)
	}
}
