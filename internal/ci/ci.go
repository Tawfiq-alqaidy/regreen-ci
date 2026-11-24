// Package ci defines provider-neutral CI types, the Provider interface, and
// git remote URL parsing. It does no I/O; adapters live in sub-packages.
package ci

import "context"

// Status is the aggregate CI state of a commit.
type Status string

const (
	// StatusSuccess means every run for the commit completed successfully.
	StatusSuccess Status = "success"
	// StatusFailure means at least one run failed.
	StatusFailure Status = "failure"
	// StatusPending means some run for the commit has not completed.
	StatusPending Status = "pending"
	// StatusNoRuns means the provider has no runs for the commit.
	StatusNoRuns Status = "no_runs"
)

// Repo identifies a repository on a CI host.
type Repo struct{ Host, Owner, Name string }

// String returns the repository as "host/owner/name".
func (r Repo) String() string { return r.Host + "/" + r.Owner + "/" + r.Name }

// FailedStep is one failed step of one job. Step is "" and StepNumber 0
// when the job failed without a failed step. Job is "" when a whole run
// failed without a failed job.
type FailedStep struct {
	ID         string // provider-scoped, stable; opaque to callers
	Workflow   string
	Job        string
	Step       string
	StepNumber int
	URL        string // human link, may be ""
}

// CommitStatus is the CI state of one commit.
type CommitStatus struct {
	Status Status
	Commit string
	Failed []FailedStep // non-empty exactly when Status == StatusFailure
}

// Provider is the brief's CIProvider.
type Provider interface {
	// Name returns the provider's short name.
	Name() string
	// CommitStatus reports the aggregate CI state of commit in repo.
	CommitStatus(ctx context.Context, repo Repo, commit string) (CommitStatus, error)
	// FailureLog returns the raw text of the failed step named by id, with
	// provider framing (timestamps, markers) removed. Untrusted data.
	FailureLog(ctx context.Context, repo Repo, id string) (string, error)
}

// ValidCommit reports whether s is a full lower-case hex object name
// (40 or 64 characters). Abbreviated or symbolic names are not accepted.
func ValidCommit(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
