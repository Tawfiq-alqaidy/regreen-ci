package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tawfiq-alqaidy/regreen-ci/internal/ci"
	gogithub "github.com/google/go-github/v93/github"
)

const (
	testSHA  = "0123456789abcdef0123456789abcdef01234567"
	otherSHA = "fedcba9876543210fedcba9876543210fedcba98"
)

var testRepo = ci.Repo{Host: "github.com", Owner: "o", Name: "r"}

func newTestProvider(t *testing.T, f *fakeGitHub) *Provider {
	t.Helper()
	p, err := New(testToken, WithBaseURL(f.baseURL()))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// noLeak fails when err mentions the token.
func noLeak(t *testing.T, err error) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), testToken) {
		t.Fatalf("error leaks the token: %v", err)
	}
}

func run(id, workflow int64, event, status, conclusion string, number int) fakeRun {
	return fakeRun{
		ID: id, Name: fmt.Sprintf("wf-%d", workflow), WorkflowID: workflow, Event: event,
		Status: status, Conclusion: conclusion, HeadSHA: testSHA, RunNumber: number, RunAttempt: 1,
		HTMLURL: fmt.Sprintf("https://example.test/run/%d", id),
	}
}

func withBranch(r fakeRun, branch string) fakeRun { r.HeadBranch = branch; return r }

func okRun(id int64) fakeRun { return run(id, id, "push", "completed", "success", 1) }

func failedRun(id int64) fakeRun { return run(id, id, "push", "completed", "failure", 1) }

func status(t *testing.T, p *Provider) ci.CommitStatus {
	t.Helper()
	got, err := p.CommitStatus(context.Background(), testRepo, testSHA)
	noLeak(t, err)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestProviderName(t *testing.T) {
	p, err := New(testToken)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != ProviderName || ProviderName != "github-actions" {
		t.Fatalf("Name = %q", p.Name())
	}
}

func TestNewRejectsEmptyToken(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Fatal("empty token accepted")
	}
}

func TestProviderDoesNotPrintToken(t *testing.T) {
	p, err := New(testToken, WithBaseURL("http://127.0.0.1:1/"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{fmt.Sprintf("%v", p), fmt.Sprintf("%+v", p), fmt.Sprintf("%#v", p), p.String()} {
		if strings.Contains(s, testToken) {
			t.Fatalf("provider prints the token: %s", s)
		}
	}
}

func TestRequestsCarryAuthAndQuery(t *testing.T) {
	f := newFakeGitHub(t)
	f.runs = []fakeRun{okRun(1)}
	p := newTestProvider(t, f)
	status(t, p)
	reqs := f.allRequests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d", len(reqs))
	}
	r := reqs[0]
	if r.Path != "/repos/o/r/actions/runs" {
		t.Fatalf("path = %q", r.Path)
	}
	if r.Auth != "Bearer "+testToken {
		t.Fatalf("Authorization = %q", r.Auth)
	}
	if r.Query["head_sha"][0] != testSHA || r.Query["per_page"][0] != "100" {
		t.Fatalf("query = %v", r.Query)
	}
}

func TestCommitStatusAggregation(t *testing.T) {
	tests := []struct {
		name string
		runs []fakeRun
		want ci.Status
	}{
		{"no runs", nil, ci.StatusNoRuns},
		{"in progress", []fakeRun{run(1, 1, "push", "in_progress", "", 1)}, ci.StatusPending},
		{"all success", []fakeRun{okRun(1), okRun(2)}, ci.StatusSuccess},
		{"skipped and neutral pass", []fakeRun{
			run(1, 1, "push", "completed", "skipped", 1),
			run(2, 2, "push", "completed", "neutral", 1),
		}, ci.StatusSuccess},
		{"rerun succeeded supersedes failure", []fakeRun{
			run(1, 1, "push", "completed", "failure", 7),
			run(2, 1, "push", "completed", "success", 8),
		}, ci.StatusSuccess},
		{"higher attempt wins on equal number", []fakeRun{
			func() fakeRun { r := run(1, 1, "push", "completed", "failure", 7); r.RunAttempt = 1; return r }(),
			func() fakeRun { r := run(1, 1, "push", "completed", "success", 7); r.RunAttempt = 2; return r }(),
		}, ci.StatusSuccess},
		{"different events are separate groups", []fakeRun{
			run(1, 1, "push", "completed", "success", 8),
			run(2, 1, "pull_request", "completed", "failure", 7),
		}, ci.StatusFailure},
		{"superseded in-progress run still counts as pending", []fakeRun{
			run(1, 1, "push", "in_progress", "", 7),
			run(2, 1, "push", "completed", "success", 8),
		}, ci.StatusPending},
		{"newer run listed first gives the same result", []fakeRun{
			run(2, 1, "push", "completed", "success", 8),
			run(1, 1, "push", "completed", "failure", 7),
		}, ci.StatusSuccess},
		{"same branch, 7 failed and 8 success", []fakeRun{
			withBranch(run(1, 1, "push", "completed", "failure", 7), "main"),
			withBranch(run(2, 1, "push", "completed", "success", 8), "main"),
		}, ci.StatusSuccess},
		{"failure on either branch counts", []fakeRun{
			withBranch(run(1, 1, "push", "completed", "failure", 7), "main"),
			withBranch(run(2, 1, "push", "completed", "success", 8), "release"),
		}, ci.StatusFailure},
		{"failure plus queued is pending", []fakeRun{
			failedRun(1), run(2, 2, "push", "queued", "", 1),
		}, ci.StatusPending},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeGitHub(t)
			f.runs = tc.runs
			p := newTestProvider(t, f)
			got := status(t, p)
			if got.Status != tc.want || got.Commit != testSHA {
				t.Fatalf("got %+v, want status %s", got, tc.want)
			}
			if got.Status != ci.StatusFailure && len(got.Failed) != 0 {
				t.Fatalf("non-failure with Failed: %+v", got.Failed)
			}
			if n := f.requestCount("/jobs"); tc.want == ci.StatusPending && n != 0 {
				t.Fatalf("jobs endpoint called %d times", n)
			}
		})
	}
}

func TestFailedStepEntry(t *testing.T) {
	f := newFakeGitHub(t)
	f.runs = []fakeRun{failedRun(100)}
	f.jobs[100] = []fakeJob{{
		ID: 200, Name: "build", Status: "completed", Conclusion: "failure", HTMLURL: "https://example.test/job/200",
		Steps: []fakeStep{
			{Name: "checkout", Number: 1, Status: "completed", Conclusion: "success"},
			{Name: "go test", Number: 2, Status: "completed", Conclusion: "failure"},
		},
	}}
	got := status(t, newTestProvider(t, f))
	want := ci.FailedStep{
		ID: "gha-100-200-2", Workflow: "wf-100", Job: "build", Step: "go test", StepNumber: 2,
		URL: "https://example.test/job/200",
	}
	if got.Status != ci.StatusFailure || len(got.Failed) != 1 || !reflect.DeepEqual(got.Failed[0], want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	reqs := f.allRequests()
	jr := reqs[len(reqs)-1]
	if jr.Path != "/repos/o/r/actions/runs/100/jobs" || jr.Query["filter"][0] != "latest" || jr.Query["per_page"][0] != "100" {
		t.Fatalf("jobs request = %+v", jr)
	}
	if jr.Auth != "Bearer "+testToken {
		t.Fatalf("jobs Authorization = %q", jr.Auth)
	}
}

func TestFailedShapes(t *testing.T) {
	failedJob := func(id int64, name string, steps ...fakeStep) fakeJob {
		return fakeJob{ID: id, Name: name, Status: "completed", Conclusion: "failure", HTMLURL: fmt.Sprintf("https://example.test/job/%d", id), Steps: steps}
	}
	step := func(name string, n int64, conclusion string) fakeStep {
		return fakeStep{Name: name, Number: n, Status: "completed", Conclusion: conclusion}
	}
	tests := []struct {
		name string
		jobs []fakeJob
		want []ci.FailedStep
	}{
		{"two failed steps in one job",
			[]fakeJob{failedJob(10, "j", step("a", 1, "failure"), step("b", 2, "success"), step("c", 3, "failure"))},
			[]ci.FailedStep{
				{ID: "gha-5-10-1", Workflow: "wf-5", Job: "j", Step: "a", StepNumber: 1, URL: "https://example.test/job/10"},
				{ID: "gha-5-10-3", Workflow: "wf-5", Job: "j", Step: "c", StepNumber: 3, URL: "https://example.test/job/10"},
			}},
		{"two failed jobs, passing job skipped",
			[]fakeJob{
				failedJob(10, "j1", step("a", 1, "failure")),
				{ID: 11, Name: "ok", Status: "completed", Conclusion: "success", Steps: []fakeStep{step("x", 1, "success")}},
				failedJob(12, "j2", step("b", 4, "failure")),
			},
			[]ci.FailedStep{
				{ID: "gha-5-10-1", Workflow: "wf-5", Job: "j1", Step: "a", StepNumber: 1, URL: "https://example.test/job/10"},
				{ID: "gha-5-12-4", Workflow: "wf-5", Job: "j2", Step: "b", StepNumber: 4, URL: "https://example.test/job/12"},
			}},
		{"cancelled job without failed step",
			[]fakeJob{{ID: 10, Name: "j", Status: "completed", Conclusion: "cancelled", HTMLURL: "https://example.test/job/10",
				Steps: []fakeStep{step("a", 1, "success"), step("b", 2, "cancelled")}}},
			[]ci.FailedStep{{ID: "gha-5-10-0", Workflow: "wf-5", Job: "j", URL: "https://example.test/job/10"}}},
		{"run failed with no jobs",
			nil,
			[]ci.FailedStep{{ID: "gha-5-0-0", Workflow: "wf-5", URL: "https://example.test/run/5"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeGitHub(t)
			r := failedRun(5)
			r.Conclusion = "failure"
			f.runs = []fakeRun{r}
			f.jobs[5] = tc.jobs
			got := status(t, newTestProvider(t, f))
			if got.Status != ci.StatusFailure || !reflect.DeepEqual(got.Failed, tc.want) {
				t.Fatalf("got %+v\nwant %+v", got.Failed, tc.want)
			}
		})
	}
}

func TestFailingRunsInAscendingRunIDOrder(t *testing.T) {
	f := newFakeGitHub(t)
	f.runs = []fakeRun{failedRun(30), failedRun(10), failedRun(20)}
	got := status(t, newTestProvider(t, f))
	var ids []string
	for _, s := range got.Failed {
		ids = append(ids, s.ID)
	}
	if fmt.Sprint(ids) != "[gha-10-0-0 gha-20-0-0 gha-30-0-0]" {
		t.Fatalf("ids = %v", ids)
	}
}

func TestRunsPagination(t *testing.T) {
	f := newFakeGitHub(t)
	for i := int64(1); i <= 150; i++ {
		f.runs = append(f.runs, okRun(i))
	}
	f.runs[149] = failedRun(150) // only visible on page 2
	got := status(t, newTestProvider(t, f))
	if got.Status != ci.StatusFailure || len(got.Failed) != 1 || got.Failed[0].ID != "gha-150-0-0" {
		t.Fatalf("got %+v", got)
	}
	if n := f.requestCount("/actions/runs"); n != 2 {
		t.Fatalf("runs requests = %d", n)
	}
}

func TestJobsPagination(t *testing.T) {
	f := newFakeGitHub(t)
	f.runs = []fakeRun{failedRun(5)}
	for i := int64(1); i <= 120; i++ {
		j := fakeJob{ID: i, Name: "j", Status: "completed", Conclusion: "success"}
		if i == 120 {
			j.Conclusion = "failure"
			j.Steps = []fakeStep{{Name: "s", Number: 3, Status: "completed", Conclusion: "failure"}}
		}
		f.jobs[5] = append(f.jobs[5], j)
	}
	got := status(t, newTestProvider(t, f))
	if len(got.Failed) != 1 || got.Failed[0].ID != "gha-5-120-3" {
		t.Fatalf("got %+v", got.Failed)
	}
}

func TestTooManyRunPages(t *testing.T) {
	f := newFakeGitHub(t)
	for i := int64(1); i <= 1001; i++ {
		f.runs = append(f.runs, okRun(i))
	}
	_, err := newTestProvider(t, f).CommitStatus(context.Background(), testRepo, testSHA)
	if err == nil || !strings.Contains(err.Error(), "too many workflow runs") {
		t.Fatalf("err = %v", err)
	}
	noLeak(t, err)
}

func TestRunWithOtherHeadSHAIsIgnored(t *testing.T) {
	f := newFakeGitHub(t)
	other := failedRun(2)
	other.HeadSHA = otherSHA
	f.runs = []fakeRun{okRun(1), other}
	f.ignoreSHAFilter = true
	if got := status(t, newTestProvider(t, f)); got.Status != ci.StatusSuccess {
		t.Fatalf("got %+v", got)
	}
	// Only foreign runs: nothing is left, so no runs.
	f.runs = []fakeRun{other}
	if got := status(t, newTestProvider(t, f)); got.Status != ci.StatusNoRuns {
		t.Fatalf("got %+v", got)
	}
}

func TestCommitStatusErrors(t *testing.T) {
	t.Run("404 stays reachable", func(t *testing.T) {
		f := newFakeGitHub(t)
		f.fail = func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, map[string]string{"message": "Not Found"})
		}
		_, err := newTestProvider(t, f).CommitStatus(context.Background(), testRepo, testSHA)
		noLeak(t, err)
		var er *gogithub.ErrorResponse
		if !errors.As(err, &er) || er.Response.StatusCode != http.StatusNotFound {
			t.Fatalf("err = %v", err)
		}
		if !strings.Contains(err.Error(), "github.com/o/r") {
			t.Fatalf("error lacks repo: %v", err)
		}
	})
	t.Run("rate limit stays reachable", func(t *testing.T) {
		f := newFakeGitHub(t)
		f.fail = func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Limit", "60")
			w.Header().Set("X-RateLimit-Reset", "4102444800")
			w.WriteHeader(http.StatusForbidden)
			writeJSON(w, map[string]string{"message": "API rate limit exceeded"})
		}
		_, err := newTestProvider(t, f).CommitStatus(context.Background(), testRepo, testSHA)
		noLeak(t, err)
		var rl *gogithub.RateLimitError
		if !errors.As(err, &rl) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("jobs error is wrapped", func(t *testing.T) {
		f := newFakeGitHub(t)
		f.runs = []fakeRun{failedRun(5)}
		f.fail = nil
		p := newTestProvider(t, f)
		f.mu.Lock()
		f.fail = func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/jobs") {
				w.WriteHeader(http.StatusNotFound)
				writeJSON(w, map[string]string{"message": "Not Found"})
				return
			}
			f.serveRuns(w, r)
		}
		f.mu.Unlock()
		_, err := p.CommitStatus(context.Background(), testRepo, testSHA)
		noLeak(t, err)
		var er *gogithub.ErrorResponse
		if !errors.As(err, &er) || !strings.Contains(err.Error(), "list jobs for github.com/o/r") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("cancelled context", func(t *testing.T) {
		f := newFakeGitHub(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := newTestProvider(t, f).CommitStatus(ctx, testRepo, testSHA)
		noLeak(t, err)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestCommitStatusValidation(t *testing.T) {
	tests := []struct {
		name   string
		repo   ci.Repo
		commit string
		want   string
	}{
		{"other host", ci.Repo{Host: "gitlab.com", Owner: "o", Name: "r"}, testSHA, `host "gitlab.com" is not supported: only github.com`},
		{"empty owner", ci.Repo{Host: "github.com", Name: "r"}, testSHA, "owner"},
		{"empty name", ci.Repo{Host: "github.com", Owner: "o"}, testSHA, "name"},
		{"short commit", testRepo, "abc123", "commit must be a full 40 or 64 character hex object name"},
		{"symbolic commit", testRepo, "main", "commit must be a full 40 or 64 character hex object name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeGitHub(t)
			_, err := newTestProvider(t, f).CommitStatus(context.Background(), tc.repo, tc.commit)
			noLeak(t, err)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if len(f.allRequests()) != 0 {
				t.Fatal("validation failure still hit the API")
			}
		})
	}
}

func TestRunPageBoundary(t *testing.T) {
	f := newFakeGitHub(t)
	for i := int64(1); i <= 1000; i++ {
		f.runs = append(f.runs, okRun(i))
	}
	if got := status(t, newTestProvider(t, f)); got.Status != ci.StatusSuccess {
		t.Fatalf("got %+v", got)
	}
	if n := f.requestCount("/actions/runs"); n != 10 {
		t.Fatalf("runs requests = %d, want exactly 10", n)
	}
}

func TestTooManyJobPages(t *testing.T) {
	f := newFakeGitHub(t)
	f.runs = []fakeRun{failedRun(5)}
	for i := int64(1); i <= 1001; i++ {
		f.jobs[5] = append(f.jobs[5], fakeJob{ID: i, Name: "j", Status: "completed", Conclusion: "success"})
	}
	_, err := newTestProvider(t, f).CommitStatus(context.Background(), testRepo, testSHA)
	if err == nil || !strings.Contains(err.Error(), "too many jobs") {
		t.Fatalf("err = %v", err)
	}
	noLeak(t, err)
}

func TestIncompleteListsAreErrors(t *testing.T) {
	t.Run("runs", func(t *testing.T) {
		f := newFakeGitHub(t)
		f.runs = []fakeRun{okRun(1)}
		f.runsTotal = 1200
		_, err := newTestProvider(t, f).CommitStatus(context.Background(), testRepo, testSHA)
		if err == nil || !strings.Contains(err.Error(), "workflow runs list is incomplete") {
			t.Fatalf("err = %v", err)
		}
		noLeak(t, err)
	})
	t.Run("jobs", func(t *testing.T) {
		f := newFakeGitHub(t)
		f.runs = []fakeRun{failedRun(5)}
		f.jobs[5] = []fakeJob{{ID: 1, Name: "j", Status: "completed", Conclusion: "success"}}
		f.jobsTotal = 500
		_, err := newTestProvider(t, f).CommitStatus(context.Background(), testRepo, testSHA)
		if err == nil || !strings.Contains(err.Error(), "jobs list is incomplete") {
			t.Fatalf("err = %v", err)
		}
		noLeak(t, err)
	})
}

type countingTransport struct {
	n    atomic.Int64
	next http.RoundTripper
}

func (c *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.n.Add(1)
	return c.next.RoundTrip(r)
}

func TestWithHTTPClientIsUsedAndNotModified(t *testing.T) {
	f := newFakeGitHub(t)
	f.runs = []fakeRun{okRun(1)}
	ct := &countingTransport{next: http.DefaultTransport}
	hc := &http.Client{Transport: ct, Timeout: 7 * time.Second}
	p, err := New(testToken, WithBaseURL(f.baseURL()), WithHTTPClient(hc))
	if err != nil {
		t.Fatal(err)
	}
	if got := status(t, p); got.Status != ci.StatusSuccess {
		t.Fatalf("got %+v", got)
	}
	if ct.n.Load() == 0 {
		t.Fatal("supplied transport was not used")
	}
	if hc.Timeout != 7*time.Second || hc.Transport != http.RoundTripper(ct) {
		t.Fatalf("caller's client was modified: timeout %v", hc.Timeout)
	}
	if reqs := f.allRequests(); reqs[0].Auth != "Bearer "+testToken {
		t.Fatalf("Authorization = %q", reqs[0].Auth)
	}
}
