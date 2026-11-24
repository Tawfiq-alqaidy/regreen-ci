package policy

import (
	"slices"
	"strings"
)

// alwaysProtected are paths no fix may touch, whatever the configuration.
// They are matched case-insensitively and cannot be removed by any caller.
var alwaysProtected = []string{
	"regreen.yml",
	"regreen.yaml",
	".regreen",
	".github",
	".git",
	// .gitmodules sets submodule URLs, and "ignore = all" in it hides
	// submodule changes from git diff.
	".gitmodules",
	".githooks",
	".husky",
	".pre-commit-config.yaml",
	"lefthook.yml",
	".lefthook.yml",
}

// IsProtected reports whether path is covered by an always-protected pattern
// or by one of extra, and returns the pattern that matched ("" if none).
// Always-protected patterns match case-insensitively; extra patterns match
// case-sensitively, and invalid extra patterns are skipped.
func IsProtected(p string, extra []string) (string, bool) {
	lowerPath := strings.ToLower(p)
	for _, pattern := range alwaysProtected {
		if MatchPath(strings.ToLower(pattern), lowerPath) {
			return pattern, true
		}
	}
	for _, pattern := range extra {
		if ValidatePattern(pattern) != nil {
			continue
		}
		if MatchPath(pattern, p) {
			return pattern, true
		}
	}
	return "", false
}

// AlwaysProtected returns a copy of the hardcoded patterns, for display.
func AlwaysProtected() []string {
	return slices.Clone(alwaysProtected)
}
