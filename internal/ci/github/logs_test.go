package github

import (
	"context"
	"errors"
	"fmt"
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
	}{
		{"zero start", lint, time.Time{}, step3End, wholeLint},
		{"zero end", lint, step3Start, time.Time{}, wholeLint},
		{"end before start", lint, step3End, step3Start, wholeLint},
		{"no timestamps", plain, step3Start, step3End, wholePlain},
		{"window matches nothing", lint, t0.Add(time.Hour), t0.Add(2 * time.Hour), wholeLint},
		{"BOM removed", bom + "2026-11-24T10:00:04.0000000Z a\n", step3Start, step3End, "2026-11-24T10:00:04.0000000Z a\n"},
		{"one BOM only", bom + bom + "x\n", time.Time{}, time.Time{}, bom + "x\n"},
		{
			"continuation lines stay with their line",
			"2026-11-24T10:00:03.0000000Z before\n" +
				"2026-11-24T10:00:05.0000000Z first\nsecond\n  third\n" +
				"2026-11-24T10:00:06.0000000Z next\n" +
				"2026-11-24T10:00:10.0000000Z after\ntail of after\n",
			step3Start, step3End,
			"2026-11-24T10:00:05.0000000Z first\nsecond\n  third\n2026-11-24T10:00:06.0000000Z next\n",
		},
		{
			"untimestamped lines before the first timestamp are dropped",
			"orphan\n2026-11-24T10:00:05.0000000Z kept\n",
			step3Start, step3End,
			"2026-11-24T10:00:05.0000000Z kept\n",
		},
		{
			"start is inclusive, end plus one second exclusive",
			"2026-11-24T10:00:03.9999999Z a\n2026-11-24T10:00:04.0000000Z b\n2026-11-24T10:00:09.9999999Z c\n2026-11-24T10:00:10.0000000Z d\n",
			step3Start, step3End,
			"2026-11-24T10:00:04.0000000Z b\n2026-11-24T10:00:09.9999999Z c\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stepSection(tt.raw, tt.start, tt.end); got != tt.want {
				t.Errorf("stepSection =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestStepSectionSelectsStepThreeOfTheFixture(t *testing.T) {
	got := stepSection(readFixture(t, "job_lint_failed.log"), step3Start, step3End)
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

func failureLog(t *testing.T, p *Provider, id string) (string, error) {
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
	got, err := failureLog(t, p, encodeID(lintRun, lintJob, 3))
	if err != nil {
		t.Fatal(err)
	}
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
			if r.Query["sig"][0] != "SECRETSIG" {
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

func TestFailureLogWholeLogCases(t *testing.T) {
	tests := []struct{ name, id string }{
		{"step 0 means no step", encodeID(lintRun, lintJob, 0)},
		{"unknown step number", encodeID(lintRun, lintJob, 99)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, p := lintFake(t)
			got, err := failureLog(t, p, tt.id)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"Set up job", "src/secret.ts", "src/a.ts", "Post job cleanup", "error: Process completed"} {
				if !strings.Contains(got, want) {
					t.Errorf("whole log misses %q", want)
				}
			}
			for _, bad := range []string{"##[", "2026-11-24T", bom} {
				if strings.Contains(got, bad) {
					t.Errorf("log still has framing %q", bad)
				}
			}
		})
	}
}

func TestFailureLogRunLevelFailureHasNoLog(t *testing.T) {
	f, p := lintFake(t)
	got, err := failureLog(t, p, encodeID(lintRun, 0, 0))
	if err != nil || got != "" {
		t.Fatalf("got %q, %v; want empty, nil", got, err)
	}
	if n := len(f.allRequests()); n != 0 {
		t.Errorf("%d requests made for a run-level failure", n)
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
				t.Fatalf("no error, got %q", got)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
			if got != "" {
				t.Errorf("returned text %q alongside an error", got)
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

	got, err := failureLog(t, p, encodeID(lintRun, lintJob, 0))
	if err != nil {
		t.Fatal(err)
	}
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
		name string
		in   string
		max  int
		want string
	}{
		{"under the cap", lines(3), 100, lines(3)},
		{"exactly the cap", lines(5), 20, lines(5)},
		{"over the cap drops the partial first line", lines(10), 14, "l07\nl08\nl09\n"},
		{"many reads", lines(5000), 4000, lines(5000)[len(lines(5000))-3996:]},
		{"empty", "", 10, ""},
		{"no newline in the tail keeps the tail", strings.Repeat("a", 50), 10, strings.Repeat("a", 10)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readTail(strings.NewReader(tt.in), tt.max)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("readTail = %q, want %q", got, tt.want)
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
	id := encodeID(lintRun, lintJob, 3)
	log, err := failureLog(t, p, id)
	if err != nil {
		t.Fatal(err)
	}
	step := ci.FailedStep{ID: id, Workflow: "CI", Job: "lint", Step: "Run npm run lint", StepNumber: 3}
	checks := []config.Check{{Name: "lint", Run: "npm run lint"}}
	known := map[string]struct{}{"src/a.ts": {}, "src/secret.ts": {}}

	got := failure.Build(step, testSHA, log, checks, known, failure.DefaultLimits())
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
