package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/Tawfiq-alqaidy/regreen-ci/internal/ci"
	gogithub "github.com/google/go-github/v93/github"
)

// ProviderName is the name reported by Provider.Name.
const ProviderName = "github-actions"

const (
	hostGitHub  = "github.com"
	perPage     = 100
	maxPages    = 10
	httpTimeout = 30 * time.Second
	userAgent   = "regreen-ci"
)

type options struct {
	baseURL    string
	httpClient *http.Client
}

// Option configures New.
type Option func(*options)

// WithBaseURL overrides the API base URL, for tests. It must end with "/".
func WithBaseURL(u string) Option { return func(o *options) { o.baseURL = u } }

// WithHTTPClient sets the transport for API calls, for tests. go-github
// makes a shallow copy of c and applies the timeout and credential
// transport to the copy; the caller's client is not modified.
func WithHTTPClient(c *http.Client) Option { return func(o *options) { o.httpClient = c } }

// Provider is the GitHub Actions implementation of ci.Provider. The token
// lives only inside the go-github client.
type Provider struct {
	client     *gogithub.Client
	baseCustom bool // base URL was overridden; log fetching consults this
}

// New returns a provider that authenticates with token.
func New(token string, opts ...Option) (*Provider, error) {
	if token == "" {
		return nil, errors.New("github token is empty")
	}
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	copts := []gogithub.ClientOptionsFunc{
		gogithub.WithAuthToken(token),
		gogithub.WithUserAgent(userAgent),
		gogithub.WithTimeout(httpTimeout),
	}
	if o.baseURL != "" {
		copts = append(copts, gogithub.WithURLs(&o.baseURL, nil))
	}
	if o.httpClient != nil {
		copts = append(copts, gogithub.WithHTTPClient(o.httpClient))
	}
	client, err := gogithub.NewClient(copts...)
	if err != nil {
		return nil, fmt.Errorf("create GitHub client: %w", err)
	}
	return &Provider{client: client, baseCustom: o.baseURL != ""}, nil
}

// String keeps %v and %+v from printing anything but the provider's name.
func (p *Provider) String() string { return "github-actions provider" }

// Name returns ProviderName.
func (p *Provider) Name() string { return ProviderName }

// runGroup identifies one line of runs: reruns and newer pushes replace
// older runs only within the same workflow, event and branch.
type runGroup struct {
	workflowID int64
	event      string
	branch     string
}

// validateRepo accepts only a complete github.com repository.
func validateRepo(repo ci.Repo) error {
	if repo.Host != hostGitHub {
		return fmt.Errorf("host %q is not supported: only github.com", repo.Host)
	}
	if repo.Owner == "" || repo.Name == "" {
		return fmt.Errorf("repository owner and name must not be empty: %q", repo.String())
	}
	return nil
}

// CommitStatus reports the aggregate GitHub Actions state of commit.
func (p *Provider) CommitStatus(ctx context.Context, repo ci.Repo, commit string) (ci.CommitStatus, error) {
	if err := validateRepo(repo); err != nil {
		return ci.CommitStatus{}, err
	}
	if !ci.ValidCommit(commit) {
		return ci.CommitStatus{}, errors.New("commit must be a full 40 or 64 character hex object name")
	}

	runs, err := p.listRuns(ctx, repo, commit)
	if err != nil {
		return ci.CommitStatus{}, err
	}
	if len(runs) == 0 {
		return ci.CommitStatus{Status: ci.StatusNoRuns, Commit: commit}, nil
	}

	// Pending until every run for the commit has completed, including runs
	// a newer run of the same group has superseded.
	for _, r := range runs {
		if r.GetStatus() != "completed" {
			return ci.CommitStatus{Status: ci.StatusPending, Commit: commit}, nil
		}
	}
	latest := latestPerGroup(runs)

	var failing []*gogithub.WorkflowRun
	for _, r := range latest {
		if !passes(r.GetConclusion()) {
			failing = append(failing, r)
		}
	}
	if len(failing) == 0 {
		return ci.CommitStatus{Status: ci.StatusSuccess, Commit: commit}, nil
	}
	slices.SortFunc(failing, func(a, b *gogithub.WorkflowRun) int {
		return cmpInt64(a.GetID(), b.GetID())
	})

	var failed []ci.FailedStep
	for _, r := range failing {
		steps, err := p.failedSteps(ctx, repo, r)
		if err != nil {
			return ci.CommitStatus{}, err
		}
		failed = append(failed, steps...)
	}
	return ci.CommitStatus{Status: ci.StatusFailure, Commit: commit, Failed: failed}, nil
}

func (p *Provider) listRuns(ctx context.Context, repo ci.Repo, commit string) ([]*gogithub.WorkflowRun, error) {
	opts := &gogithub.ListWorkflowRunsOptions{
		HeadSHA:     commit,
		ListOptions: gogithub.ListOptions{PerPage: perPage},
	}
	var runs []*gogithub.WorkflowRun
	received := 0
	for pages := 0; ; pages++ {
		if pages == maxPages {
			return nil, fmt.Errorf("too many workflow runs for %s: more than %d pages", repo, maxPages)
		}
		res, resp, err := p.client.Actions.ListRepositoryWorkflowRuns(ctx, repo.Owner, repo.Name, opts)
		if err != nil {
			return nil, fmt.Errorf("list workflow runs for %s: %w", repo, err)
		}
		received += len(res.WorkflowRuns)
		for _, r := range res.WorkflowRuns {
			if r.GetHeadSHA() == commit {
				runs = append(runs, r)
			}
		}
		if resp.NextPage == 0 {
			// GitHub caps how many results a listing can return; refuse
			// to decide on partial data.
			if res.GetTotalCount() > received {
				return nil, fmt.Errorf("workflow runs list is incomplete for %s: received %d of %d", repo, received, res.GetTotalCount())
			}
			return runs, nil
		}
		opts.Page = resp.NextPage
	}
}

// latestPerGroup keeps, for each (workflow, event, branch), the run with the highest
// run number, then the highest attempt.
func latestPerGroup(runs []*gogithub.WorkflowRun) []*gogithub.WorkflowRun {
	best := map[runGroup]*gogithub.WorkflowRun{}
	var order []runGroup
	for _, r := range runs {
		k := runGroup{r.GetWorkflowID(), r.GetEvent(), r.GetHeadBranch()}
		cur, ok := best[k]
		if !ok {
			order = append(order, k)
		}
		if !ok || r.GetRunNumber() > cur.GetRunNumber() ||
			(r.GetRunNumber() == cur.GetRunNumber() && r.GetRunAttempt() > cur.GetRunAttempt()) {
			best[k] = r
		}
	}
	out := make([]*gogithub.WorkflowRun, 0, len(order))
	for _, k := range order {
		out = append(out, best[k])
	}
	return out
}

func passes(conclusion string) bool {
	return conclusion == "success" || conclusion == "skipped" || conclusion == "neutral"
}

func (p *Provider) failedSteps(ctx context.Context, repo ci.Repo, run *gogithub.WorkflowRun) ([]ci.FailedStep, error) {
	jobs, err := p.listJobs(ctx, repo, run)
	if err != nil {
		return nil, err
	}
	var out []ci.FailedStep
	for _, job := range jobs {
		if job.GetStatus() != "completed" || passes(job.GetConclusion()) {
			continue
		}
		before := len(out)
		for _, step := range job.Steps {
			if step.GetConclusion() != "failure" {
				continue
			}
			n := int(step.GetNumber())
			out = append(out, ci.FailedStep{
				ID:         encodeID(run.GetID(), job.GetID(), n),
				Workflow:   run.GetName(),
				Job:        job.GetName(),
				Step:       step.GetName(),
				StepNumber: n,
				URL:        job.GetHTMLURL(),
			})
		}
		if len(out) == before {
			out = append(out, ci.FailedStep{
				ID:       encodeID(run.GetID(), job.GetID(), 0),
				Workflow: run.GetName(),
				Job:      job.GetName(),
				URL:      job.GetHTMLURL(),
			})
		}
	}
	if len(out) == 0 {
		out = append(out, ci.FailedStep{
			ID:       encodeID(run.GetID(), 0, 0),
			Workflow: run.GetName(),
			URL:      run.GetHTMLURL(),
		})
	}
	return out, nil
}

func (p *Provider) listJobs(ctx context.Context, repo ci.Repo, run *gogithub.WorkflowRun) ([]*gogithub.WorkflowJob, error) {
	opts := &gogithub.ListWorkflowJobsOptions{
		Filter:      "latest",
		ListOptions: gogithub.ListOptions{PerPage: perPage},
	}
	var jobs []*gogithub.WorkflowJob
	for pages := 0; ; pages++ {
		if pages == maxPages {
			return nil, fmt.Errorf("too many jobs for %s run %d: more than %d pages", repo, run.GetID(), maxPages)
		}
		res, resp, err := p.client.Actions.ListWorkflowJobs(ctx, repo.Owner, repo.Name, run.GetID(), opts)
		if err != nil {
			return nil, fmt.Errorf("list jobs for %s run %d: %w", repo, run.GetID(), err)
		}
		jobs = append(jobs, res.Jobs...)
		if resp.NextPage == 0 {
			if res.GetTotalCount() > len(jobs) {
				return nil, fmt.Errorf("jobs list is incomplete for %s run %d: received %d of %d", repo, run.GetID(), len(jobs), res.GetTotalCount())
			}
			return jobs, nil
		}
		opts.Page = resp.NextPage
	}
}

func cmpInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
