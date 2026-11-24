package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	HeadBranch string `json:"head_branch"`
	RunNumber  int    `json:"run_number"`
	RunAttempt int    `json:"run_attempt"`
	HTMLURL    string `json:"html_url"`
}

type fakeStep struct {
	Name       string `json:"name"`
	Number     int64  `json:"number"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion,omitempty"`

	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
}

type fakeJob struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	Conclusion string     `json:"conclusion,omitempty"`
	HTMLURL    string     `json:"html_url"`
	Steps      []fakeStep `json:"steps"`
	RunID      int64      `json:"run_id,omitempty"`

	HeadSHA      string `json:"head_sha,omitempty"`
	WorkflowName string `json:"workflow_name,omitempty"`
}

type fakeRequest struct {
	Path  string
	Query url.Values
	Auth  string
}

// fakeGitHub is a small in-memory GitHub Actions API.
type fakeGitHub struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	runs     []fakeRun
	jobs     map[int64][]fakeJob
	jobByID  map[int64]fakeJob
	logs     map[int64][]byte // job log bodies, served from /download/{id}
	requests []fakeRequest

	// downloadStatus, when set for a job, makes its download answer with
	// that status instead of the log body.
	downloadStatus map[int64]int

	// logLocation, when non-empty, replaces the redirect target of every
	// job log request.
	logLocation string

	// noLocation makes job log requests answer 302 without a Location.
	noLocation bool

	// emptyLocation makes job log requests answer 302 with an empty
	// Location header.
	emptyLocation bool

	// slowHit receives a value whenever /slow is requested.
	slowHit chan struct{}

	// ignoreSHAFilter makes the fake return every run regardless of
	// head_sha, to test the client's defensive filtering.
	ignoreSHAFilter bool

	// runsTotal and jobsTotal, when non-zero, override total_count.
	runsTotal, jobsTotal int

	// fail, when non-nil, answers every request instead of the fixtures.
	fail func(w http.ResponseWriter, r *http.Request)
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{
		t: t, jobs: map[int64][]fakeJob{}, jobByID: map[int64]fakeJob{},
		logs: map[int64][]byte{}, downloadStatus: map[int64]int{}, slowHit: make(chan struct{}, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/{owner}/{repo}/actions/runs", f.serveRuns)
	mux.HandleFunc("GET /repos/{owner}/{repo}/actions/runs/{id}/jobs", f.serveJobs)
	mux.HandleFunc("GET /repos/{owner}/{repo}/actions/jobs/{id}", f.serveJob)
	mux.HandleFunc("GET /repos/{owner}/{repo}/actions/jobs/{id}/logs", f.serveJobLogs)
	mux.HandleFunc("GET /download/{id}", f.serveDownload)
	mux.HandleFunc("GET /slow", f.serveSlow)
	mux.HandleFunc("GET /chain/{n}/{id}", f.serveChain)
	mux.HandleFunc("GET /redirect", f.serveRedirect)
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
	total := len(match)
	if f.runsTotal != 0 {
		total = f.runsTotal
	}
	writeJSON(w, map[string]any{"total_count": total, "workflow_runs": match[lo:hi]})
}

func (f *fakeGitHub) serveJobs(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	f.mu.Lock()
	jobs := f.jobs[id]
	f.mu.Unlock()
	lo, hi := page(w, r, len(jobs))
	total := len(jobs)
	if f.jobsTotal != 0 {
		total = f.jobsTotal
	}
	writeJSON(w, map[string]any{"total_count": total, "jobs": jobs[lo:hi]})
}

func (f *fakeGitHub) serveJob(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	f.mu.Lock()
	job, ok := f.jobByID[id]
	f.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, job)
}

// serveJobLogs answers like GitHub: a 302 to a signed download URL on
// another path. The signature must never show up in an error.
func (f *fakeGitHub) serveJobLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	loc := fmt.Sprintf("http://%s/download/%s?sig=SECRETSIG", r.Host, id)
	f.mu.Lock()
	if f.logLocation != "" {
		loc = f.logLocation
	}
	noLocation, emptyLocation := f.noLocation, f.emptyLocation
	f.mu.Unlock()
	switch {
	case emptyLocation:
		w.Header()["Location"] = []string{""}
	case !noLocation:
		w.Header().Set("Location", loc)
	}
	w.WriteHeader(http.StatusFound)
}

func (f *fakeGitHub) serveDownload(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	f.mu.Lock()
	body, ok := f.logs[id]
	status := f.downloadStatus[id]
	f.mu.Unlock()
	if status != 0 {
		http.Error(w, "no log", status)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	_, _ = w.Write(body)
}

// serveChain redirects n more times (keeping the signature) and then serves
// the job's log like /download/{id}.
func (f *fakeGitHub) serveChain(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(r.PathValue("n"))
	id := r.PathValue("id")
	if n <= 0 {
		r.SetPathValue("id", id)
		f.serveDownload(w, r)
		return
	}
	w.Header().Set("Location", fmt.Sprintf("/chain/%d/%s?sig=SECRETSIG", n-1, id))
	w.WriteHeader(http.StatusFound)
}

// serveRedirect answers 302 with the raw Location given in ?to=.
func (f *fakeGitHub) serveRedirect(w http.ResponseWriter, r *http.Request) {
	w.Header()["Location"] = []string{r.URL.Query().Get("to")}
	w.WriteHeader(http.StatusFound)
}

// serveSlow announces itself on slowHit, then holds the request open until
// the client gives up.
func (f *fakeGitHub) serveSlow(_ http.ResponseWriter, r *http.Request) {
	select {
	case f.slowHit <- struct{}{}:
	default:
	}
	<-r.Context().Done()
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
