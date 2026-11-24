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

// StepLog is what a provider knows about one failed step, resolved from its ID.
type StepLog struct {
	Step    FailedStep // rebuilt from the provider's own records, not from the caller
	Commit  string     // full object name the job ran for
	Log     string     // raw text of the step, provider framing removed; untrusted
	Partial bool       // true when Log is known to be incomplete or not limited to the step
}

// Provider is a CI system regreen can ask about a commit: it reports the
// aggregate state of the commit's runs, lists the failed steps, and fetches
// the log of one failed step by the ID CommitStatus gave it.
type Provider interface {
	// Name returns the provider's short name.
	Name() string
	// CommitStatus reports the aggregate CI state of commit in repo.
	CommitStatus(ctx context.Context, repo Repo, commit string) (CommitStatus, error)
	// FailureLog resolves id, a FailedStep.ID that CommitStatus issued, to
	// the step and commit it names and the step's raw log text, with
	// provider framing (timestamps, markers) removed. The log is untrusted
	// data. An id is caller-supplied: the provider rebuilds Step and Commit
	// from its own records and returns an error when id does not name a
	// failed step. Callers must compare StepLog.Commit with the commit they
	// are working on and reject a mismatch.
	FailureLog(ctx context.Context, repo Repo, id string) (StepLog, error)
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
