package policy

import (
	"fmt"
	"path"
	"slices"
	"strings"
)

// EditScope says which files a fix may edit.
type EditScope string

// The supported edit scopes.
const (
	ScopeFilesInErrors EditScope = "files_in_errors" // only files named in the errors
	ScopeAny           EditScope = "any"             // any unprotected file
)

// Rules is the project's policy. The always-protected paths are not part of
// it and cannot be turned off through it.
type Rules struct {
	ProtectedFiles    []string
	ForbiddenPatterns []string
	MaxChangedLines   int // added + deleted
	MaxDeletedLines   int
	MaxAttempts       int
	EditScope         EditScope
}

// DefaultRules returns the strict defaults: no extra protected files or
// patterns, 200 changed lines, 50 deleted lines, 2 attempts, files_in_errors.
func DefaultRules() Rules {
	return Rules{
		MaxChangedLines: 200,
		MaxDeletedLines: 50,
		MaxAttempts:     2,
		EditScope:       ScopeFilesInErrors,
	}
}

// Input is everything Evaluate looks at. Nothing the agent says about its own
// work is an input.
type Input struct {
	Diff         Diff
	Rules        Rules
	ErrorFiles   []string // repo-relative paths named in the failure's errors
	AttemptsUsed int      // counted by the caller from commit trailers in git history
}

// Rule names used in Reason.Rule.
const (
	RuleMaxAttempts      = "max_attempts"
	RuleEmptyDiff        = "empty_diff"
	RuleInvalidRules     = "invalid_rules"
	RuleInvalidPath      = "invalid_path"
	RuleInvalidDiff      = "invalid_diff"
	RuleProtectedFile    = "protected_file"
	RuleEditScope        = "edit_scope"
	RuleBinaryFile       = "binary_file"
	RuleSpecialFile      = "special_file"
	RuleForbiddenPattern = "forbidden_pattern"
	RuleMaxChangedLines  = "max_changed_lines"
	RuleMaxDeletedLines  = "max_deleted_lines"
)

// maxForbiddenReasons caps how many forbidden_pattern reasons are returned.
const maxForbiddenReasons = 20

// Reason is one rule violation.
type Reason struct {
	Rule    string // one of the Rule* constants
	File    string // "" when the reason is not about one file
	Line    int    // new-file line number, 0 when not about one line
	Message string // one sentence a fixing agent can act on
}

// Verdict is the outcome of Evaluate.
type Verdict struct {
	Allowed bool
	Reasons []Reason // empty exactly when Allowed is true
	// AttemptsRemaining is max(0, MaxAttempts-AttemptsUsed), independent of
	// Allowed. It is computed before the attempt being judged is counted: an
	// allowed submit with AttemptsUsed 1 of 2 reports 1, and once that fix is
	// pushed (and so counted), 0 remain.
	AttemptsRemaining int
}

// Evaluate decides whether the diff is allowed under the rules. It reports
// every violation it finds, never panics, and does not modify its input.
func Evaluate(in Input) Verdict {
	v := Verdict{AttemptsRemaining: max(0, in.Rules.MaxAttempts-in.AttemptsUsed)}

	if reasons := checkRules(in); len(reasons) > 0 {
		v.Reasons = reasons
		return v
	}

	var reasons []Reason
	if in.AttemptsUsed >= in.Rules.MaxAttempts {
		reasons = append(reasons, Reason{
			Rule: RuleMaxAttempts,
			Message: fmt.Sprintf("no attempts remaining (used %d of %d); stop and report to the developer",
				in.AttemptsUsed, in.Rules.MaxAttempts),
		})
	}
	if len(in.Diff.Files) == 0 {
		reasons = append(reasons, Reason{Rule: RuleEmptyDiff, Message: "the diff changes no files"})
	}

	named := cleanedSet(in.ErrorFiles)
	seen := make(map[string]bool, len(in.Diff.Files))
	for _, f := range in.Diff.Files {
		duplicate := seen[f.Path]
		seen[f.Path] = true
		reasons = append(reasons, checkFile(f, in.Rules, named, duplicate)...)
	}
	reasons = append(reasons, totals(in.Diff, in.Rules)...)

	v.Reasons = capForbidden(reasons)
	v.Allowed = len(v.Reasons) == 0
	return v
}

// capForbidden keeps the first maxForbiddenReasons forbidden_pattern reasons
// and summarises the rest in one extra reason.
func capForbidden(reasons []Reason) []Reason {
	out := make([]Reason, 0, len(reasons))
	extra := 0
	kept := 0
	for _, r := range reasons {
		if r.Rule == RuleForbiddenPattern {
			if kept == maxForbiddenReasons {
				extra++
				continue
			}
			kept++
		}
		out = append(out, r)
	}
	if extra > 0 {
		out = append(out, Reason{
			Rule:    RuleForbiddenPattern,
			Message: fmt.Sprintf("and %d more forbidden-pattern matches", extra),
		})
	}
	return out
}

func checkRules(in Input) []Reason {
	var reasons []Reason
	bad := func(format string, args ...any) {
		reasons = append(reasons, Reason{Rule: RuleInvalidRules, Message: fmt.Sprintf(format, args...)})
	}
	r := in.Rules
	if r.MaxChangedLines < 1 {
		bad("max_changed_lines is %d but must be at least 1", r.MaxChangedLines)
	}
	if r.MaxDeletedLines < 0 {
		bad("max_deleted_lines is %d but must not be negative", r.MaxDeletedLines)
	}
	if r.MaxAttempts < 1 {
		bad("max_attempts is %d but must be at least 1", r.MaxAttempts)
	}
	if r.EditScope != ScopeFilesInErrors && r.EditScope != ScopeAny {
		bad("edit_scope is %q but must be %q or %q", string(r.EditScope), string(ScopeFilesInErrors), string(ScopeAny))
	}
	for _, p := range r.ProtectedFiles {
		if err := ValidatePattern(p); err != nil {
			bad("protected_files pattern %q is not valid: %v", p, err)
		}
	}
	if slices.Contains(r.ForbiddenPatterns, "") {
		bad("forbidden_patterns contains an empty pattern")
	}
	for _, p := range r.ForbiddenPatterns {
		// Added lines never contain a newline, so such a pattern never matches.
		if strings.Contains(p, "\n") {
			bad("forbidden_patterns pattern %q contains a newline and can never match a line", p)
		}
	}
	if in.AttemptsUsed < 0 {
		bad("attempts used is %d but must not be negative", in.AttemptsUsed)
	}
	return reasons
}

func cleanedSet(files []string) map[string]bool {
	set := make(map[string]bool, len(files))
	for _, f := range files {
		if strings.TrimSpace(f) == "" {
			continue
		}
		set[path.Clean(f)] = true
	}
	return set
}

func checkFile(f FileChange, rules Rules, named map[string]bool, duplicate bool) []Reason {
	if checkRepoPath(f.Path) != nil {
		return []Reason{{
			Rule:    RuleInvalidPath,
			File:    f.Path,
			Message: fmt.Sprintf("the diff contains the path %q, which is not a clean repo-relative path", clip(f.Path)),
		}}
	}

	if problem := diffProblem(f, duplicate); problem != "" {
		return []Reason{{
			Rule:    RuleInvalidDiff,
			File:    f.Path,
			Message: fmt.Sprintf("the diff entry for %q is malformed: %s", f.Path, problem),
		}}
	}

	var reasons []Reason
	if pattern, ok := IsProtected(f.Path, rules.ProtectedFiles); ok {
		source := "protected by the project config"
		if _, always := IsProtected(f.Path, nil); always {
			source = "always protected"
		}
		reasons = append(reasons, Reason{
			Rule:    RuleProtectedFile,
			File:    f.Path,
			Message: fmt.Sprintf("%q is %s (matches %q) and cannot be changed", f.Path, source, pattern),
		})
	}
	if rules.EditScope == ScopeFilesInErrors && !named[f.Path] {
		reasons = append(reasons, Reason{Rule: RuleEditScope, File: f.Path, Message: editScopeMessage(f.Path, named)})
	}
	if f.Binary {
		reasons = append(reasons, Reason{
			Rule: RuleBinaryFile, File: f.Path,
			Message: fmt.Sprintf("%q is a binary change, which is not allowed", f.Path),
		})
	}
	if f.Special != "" {
		article := "a"
		if strings.ContainsRune("aeiou", rune(f.Special[0])) {
			article = "an"
		}
		reasons = append(reasons, Reason{
			Rule: RuleSpecialFile, File: f.Path,
			Message: fmt.Sprintf("%q is %s %s change, which is not allowed", f.Path, article, f.Special),
		})
	}
	return append(reasons, forbiddenReasons(f, rules.ForbiddenPatterns)...)
}

// specialKinds are the FileChange.Special values the parser produces.
var specialKinds = []string{"", "mode change", "symlink", "submodule", "executable"}

// diffProblem describes why a FileChange cannot come from a real git diff, or
// returns "" when it is well formed.
func diffProblem(f FileChange, duplicate bool) string {
	switch {
	case duplicate:
		return "the path appears more than once"
	case f.Kind != Modified && f.Kind != Added && f.Kind != Deleted:
		return fmt.Sprintf("unknown change kind %d", int(f.Kind))
	case f.Deleted < 0:
		return fmt.Sprintf("negative deleted line count %d", f.Deleted)
	case f.Binary && (len(f.Added) > 0 || f.Deleted > 0):
		return "a binary change cannot have line counts"
	case !slices.Contains(specialKinds, f.Special):
		return fmt.Sprintf("unknown special kind %q", f.Special)
	}
	for _, l := range f.Added {
		if l.Number < 1 {
			return fmt.Sprintf("added line has number %d", l.Number)
		}
	}
	return ""
}

func editScopeMessage(p string, named map[string]bool) string {
	if len(named) == 0 {
		return fmt.Sprintf("%q is not named in the errors; no files are named in the errors", p)
	}
	files := make([]string, 0, len(named))
	for f := range named {
		files = append(files, f)
	}
	slices.Sort(files)
	for i, f := range files {
		files[i] = fmt.Sprintf("%q", clip(f))
	}
	return fmt.Sprintf("%q is not named in the errors; only these files may be edited: %s", p, strings.Join(files, ", "))
}

func forbiddenReasons(f FileChange, patterns []string) []Reason {
	var reasons []Reason
	for _, line := range f.Added {
		for _, pattern := range patterns {
			if strings.Contains(line.Text, pattern) {
				reasons = append(reasons, Reason{
					Rule: RuleForbiddenPattern, File: f.Path, Line: line.Number,
					Message: fmt.Sprintf("added line contains forbidden pattern %q", pattern),
				})
			}
		}
	}
	return reasons
}

func totals(d Diff, rules Rules) []Reason {
	var reasons []Reason
	// A malformed negative count is reported as invalid_diff and must not
	// offset another file's lines here.
	deleted, added := 0, d.AddedLines()
	for _, f := range d.Files {
		deleted += max(0, f.Deleted)
	}
	if deleted > rules.MaxDeletedLines {
		reasons = append(reasons, Reason{
			Rule:    RuleMaxDeletedLines,
			Message: fmt.Sprintf("the diff deletes %d lines but at most %d are allowed", deleted, rules.MaxDeletedLines),
		})
	}
	if added+deleted > rules.MaxChangedLines {
		reasons = append(reasons, Reason{
			Rule: RuleMaxChangedLines,
			Message: fmt.Sprintf("the diff changes %d lines (%d added, %d deleted) but at most %d are allowed",
				added+deleted, added, deleted, rules.MaxChangedLines),
		})
	}
	return reasons
}
