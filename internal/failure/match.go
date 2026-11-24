package failure

import (
	"strings"

	"github.com/Tawfiq-alqaidy/regreen-ci/internal/config"
)

// MatchCheck finds the configured check a failed step belongs to. Levels are
// tried in order and the first that yields exactly one check wins; a level
// that yields several different checks is ambiguous and fails closed:
//
//  1. the step's command equals a check's run, or the step name equals the
//     check's name (case-insensitive);
//  2. the step's command contains a check's run at token boundaries (the
//     longest run wins);
//  3. the job name equals the check's name (case-insensitive).
//
// The step's command is its name without a leading "Run " (the default name
// of a GitHub step). Both strings are compared with whitespace collapsed.
func MatchCheck(job, step string, checks []config.Check) (config.Check, bool) {
	stepNorm := norm(step)
	stepCmd := strings.TrimPrefix(stepNorm, "Run ")
	if stepNorm != "" {
		if c, ok, decided := pick(checks, func(c config.Check) bool {
			run := norm(c.Run)
			return (run != "" && stepCmd == run) || strings.EqualFold(stepNorm, c.Name)
		}); decided {
			return c, ok
		}
		if c, ok, decided := longestContained(checks, stepCmd); decided {
			return c, ok
		}
	}
	jobNorm := norm(job)
	if jobNorm != "" {
		if c, ok, _ := pick(checks, func(c config.Check) bool {
			return strings.EqualFold(jobNorm, c.Name)
		}); ok {
			return c, true
		}
	}
	return config.Check{}, false
}

// norm trims s and collapses every whitespace run to one space.
func norm(s string) string { return strings.Join(strings.Fields(s), " ") }

// pick returns the single check satisfying match. decided is true when at
// least one check matched; ok is false when more than one did.
func pick(checks []config.Check, match func(config.Check) bool) (c config.Check, ok, decided bool) {
	n := 0
	for _, ch := range checks {
		if match(ch) {
			c = ch
			n++
		}
	}
	switch n {
	case 0:
		return config.Check{}, false, false
	case 1:
		return c, true, true
	}
	return config.Check{}, false, true
}

func longestContained(checks []config.Check, cmd string) (c config.Check, ok, decided bool) {
	best := 0
	n := 0
	for _, ch := range checks {
		run := norm(ch.Run)
		if run == "" || !containsAtBoundaries(cmd, run) {
			continue
		}
		switch {
		case len(run) > best:
			best, c, n = len(run), ch, 1
		case len(run) == best:
			n++
		}
	}
	switch n {
	case 0:
		return config.Check{}, false, false
	case 1:
		return c, true, true
	}
	return config.Check{}, false, true
}

func containsAtBoundaries(s, sub string) bool {
	for from := 0; from <= len(s)-len(sub); {
		i := strings.Index(s[from:], sub)
		if i < 0 {
			return false
		}
		i += from
		end := i + len(sub)
		if (i == 0 || s[i-1] == ' ') && (end == len(s) || s[end] == ' ') {
			return true
		}
		from = i + 1
	}
	return false
}
