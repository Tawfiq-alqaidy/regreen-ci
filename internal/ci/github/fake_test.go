package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
)

const testToken = "test-token-123"

type fakeRun struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	WorkflowID int64  `json:"workflow_id"`
	Event      string `json:"event"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion,omitempty"`
	HeadSHA    string `json:"head_sha"`
	RunNumber  int    `json:"run_number"`
	RunAttempt int    `json:"run_attempt"`
	HTMLURL    string `json:"html_url"`
}

type fakeStep struct {
	Name       string `json:"name"`
	Number     int64  `json:"number"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion,omitempty"`
}

type fakeJob struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	Conclusion string     `json:"conclusion,omitempty"`
	HTMLURL    string     `json:"html_url"`
	Steps      []fakeStep `json:"steps"`
}

type fakeRequest struct {
	Path  string
	Query map[string][]string
	Auth  string
}

// fakeGitHub is a small in-memory GitHub Actions API.
type fakeGitHub struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	runs     []fakeRun
	jobs     map[int64][]fakeJob
	requests []fakeRequest

	// ignoreSHAFilter makes the fake return every run regardless of
	// head_sha, to test the client's defensive filtering.
	ignoreSHAFilter bool

	// fail, when non-nil, answers every request instead of the fixtures.
	fail func(w http.ResponseWriter, r *http.Request)
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{t: t, jobs: map[int64][]fakeJob{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/{owner}/{repo}/actions/runs", f.serveRuns)
	mux.HandleFunc("GET /repos/{owner}/{repo}/actions/runs/{id}/jobs", f.serveJobs)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, fakeRequest{r.URL.Path, r.URL.Query(), r.Header.Get("Authorization")})
		fail := f.fail
		f.mu.Unlock()
		if fail != nil {
			fail(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// baseURL is the API base for WithBaseURL.
func (f *fakeGitHub) baseURL() string { return f.srv.URL + "/" }

func (f *fakeGitHub) requestCount(pathSuffix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.requests {
		if len(r.Path) >= len(pathSuffix) && r.Path[len(r.Path)-len(pathSuffix):] == pathSuffix {
			n++
		}
	}
	return n
}

func (f *fakeGitHub) allRequests() []fakeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeRequest(nil), f.requests...)
}

// page returns the slice for the request's page and sets the Link header.
func page(w http.ResponseWriter, r *http.Request, total int) (lo, hi int) {
	per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if per <= 0 {
		per = 30
	}
	pg, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if pg <= 0 {
		pg = 1
	}
	lo = min((pg-1)*per, total)
	hi = min(lo+per, total)
	if hi < total {
		q := r.URL.Query()
		q.Set("page", strconv.Itoa(pg+1))
		w.Header().Set("Link", fmt.Sprintf(`<http://%s%s?%s>; rel="next"`, r.Host, r.URL.Path, q.Encode()))
	}
	return lo, hi
}

func (f *fakeGitHub) serveRuns(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	var match []fakeRun
	sha := r.URL.Query().Get("head_sha")
	for _, run := range f.runs {
		if f.ignoreSHAFilter || sha == "" || run.HeadSHA == sha {
			match = append(match, run)
		}
	}
	f.mu.Unlock()
	lo, hi := page(w, r, len(match))
	writeJSON(w, map[string]any{"total_count": len(match), "workflow_runs": match[lo:hi]})
}

func (f *fakeGitHub) serveJobs(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	f.mu.Lock()
	jobs := f.jobs[id]
	f.mu.Unlock()
	lo, hi := page(w, r, len(jobs))
	writeJSON(w, map[string]any{"total_count": len(jobs), "jobs": jobs[lo:hi]})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
