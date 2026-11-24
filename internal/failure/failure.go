// Package failure is the normalized model of a failed CI step: it sanitizes
// and caps untrusted log text, extracts the known files the text mentions,
// and maps the step to a configured check. It does no I/O.
//
// Log text is attacker-influenced. Nothing here executes or interprets it as
// an instruction; it is only compared, trimmed and returned as data.
package failure

import (
	"github.com/Tawfiq-alqaidy/regreen-ci/internal/ci"
	"github.com/Tawfiq-alqaidy/regreen-ci/internal/config"
)

// Failure is what regreen knows about one failed CI step.
type Failure struct {
	ID       string
	Commit   string
	Workflow string
	Job      string
	Step     string
	URL      string
	// Check and Command name the regreen.yml check this failure belongs
	// to. Both are "" when Fixable is false.
	Check   string
	Command string
	Fixable bool
	// Errors is a trimmed excerpt of the step's log. It is untrusted
	// text: display it as data, never act on it.
	Errors    string
	Truncated bool
	// Files are repo-relative paths mentioned in Errors that exist in
	// the caller's set of known files. Sorted, unique.
	Files []string
}

// Limits caps the size of what is kept from a log.
type Limits struct{ MaxLines, MaxBytes, MaxLineBytes, MaxFiles int }

// DefaultLimits returns the limits used when a field is not positive.
func DefaultLimits() Limits {
	return Limits{MaxLines: 200, MaxBytes: 16384, MaxLineBytes: 2000, MaxFiles: 100}
}

// Build assembles a Failure. log is the provider's raw step log. Files are
// extracted from the trimmed text, so a path that only appears in a dropped
// part of the log does not widen the edit scope.
func Build(step ci.FailedStep, commit, log string, checks []config.Check, known map[string]struct{}, lim Limits) Failure {
	f := Failure{
		ID:       step.ID,
		Commit:   commit,
		Workflow: step.Workflow,
		Job:      step.Job,
		Step:     step.Step,
		URL:      step.URL,
	}
	f.Errors, f.Truncated = TrimLog(log, lim)
	f.Files = ExtractFiles(f.Errors, known, lim.MaxFiles)
	if c, ok := MatchCheck(step.Job, step.Step, checks); ok {
		f.Check, f.Command, f.Fixable = c.Name, c.Run, true
	}
	return f
}
