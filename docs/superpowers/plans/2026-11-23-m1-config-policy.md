# M1 Config and Policy Engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Load and validate `regreen.yml` and the user config, and decide with a pure policy engine whether a diff is allowed, with reasons specific enough to act on.

**Architecture:** `internal/policy` is pure (standard library only, no I/O, no globals mutated): it owns the path matcher, the always-protected paths, the diff model, a unified-diff parser, and `Evaluate`. `internal/config` does the I/O: it parses YAML strictly, applies defaults, validates, and produces a `policy.Rules`. Nothing is wired into the CLI in M1; later milestones call these packages.

**Tech Stack:** Go (version in `go.mod`), standard library, one new dependency: the YAML organization's maintained YAML library (`go.yaml.in/yaml`).

**Spec:** `docs/superpowers/specs/regreen-ci-brief.md` (M1 in section 8; sections 2, 4, 6, 7, 9)

## Global Constraints

- Module path `github.com/Tawfiq-alqaidy/regreen-ci`. Work on branch `m1-config-policy`. Never push, never touch `main` or `m0-scaffold`.
- `internal/policy` imports only the Go standard library and performs no I/O: no `os`, `io/fs`, `net`, `os/exec`, no reading of environment or time. It must not import `internal/config`.
- Fail closed. Anything the engine or parser does not understand is a rejection or an error, never an allowance.
- The always-protected paths are hardcoded inside `internal/policy` and applied by `Evaluate` itself on every call. No `Rules` value, config key, or caller option can remove or disable them.
- Never trust the agent: `Evaluate` takes only the diff, the rules, the files named in the errors, and the number of attempts already used. It has no input for anything the agent says about its work.
- Maintainer decisions (2026-11-23), binding:
  - YAML library: `go.yaml.in/yaml` (the maintained fork), strict decoding that rejects unknown keys.
  - Omitted policy keys take strict defaults: `max_changed_lines: 200`, `max_deleted_lines: 50`, `max_attempts: 2`, `edit_scope: files_in_errors`.
  - `forbidden_patterns` entries are literal substrings, matched case-sensitively against lines the diff adds.
  - `protected_files` patterns are gitignore-style (defined in Task 1). Hardcoded patterns match case-insensitively.
- Dependencies: exactly one new module in `go.mod` (the YAML library), pinned to a released (non-pre-release) version looked up at implementation time, not from memory. No other third-party module.
- Every behavior has a test. Tests are table-driven where there is more than one case, do not touch the network, and use `t.TempDir()` for files.
- Pass `context.Context` as the first parameter of every function that does I/O. Wrap errors with `fmt.Errorf("...: %w", err)` and enough context to diagnose.
- `make check` (vet, golangci-lint with no exclusions added, race tests) must exit 0 at the end of every task. Never weaken a lint rule; fix the code.
- Small focused commits with conventional prefixes. End each commit message with the attribution trailer your own harness names.
- M1 only: no CLI wiring, no git or network code, no `internal/failure`, `internal/ci`, `internal/vcs`.

## File Structure

| File | Responsibility |
|---|---|
| `internal/policy/glob.go` | `ValidatePattern`, `MatchPath`: gitignore-style path matching |
| `internal/policy/protected.go` | The hardcoded always-protected patterns and `IsProtected` |
| `internal/policy/diff.go` | Diff model: `Diff`, `FileChange`, `Line`, `ChangeKind` |
| `internal/policy/parse.go` | `ParseUnifiedDiff`: git patch text to `Diff`, fail closed |
| `internal/policy/policy.go` | `Rules`, `Input`, `Reason`, `Verdict`, `Evaluate` |
| `internal/policy/*_test.go` | Table tests per file, plus `hardening_test.go` for spec section 6 |
| `internal/config/repo.go` | `Repo`, `Check`, `ParseRepo`, `LoadRepo`: `regreen.yml` |
| `internal/config/user.go` | `User`, `DefaultUser`, `UserConfigPath`, `LoadUser` |
| `internal/config/*_test.go` | Tests |
| `docs/ARCHITECTURE.md`, `docs/DECISIONS.md`, `README.md`, `docs/milestones/M1.md` | Updated and new docs |

---

### Task 1: Path matcher and always-protected paths

**Files:**
- Create: `internal/policy/glob.go`, `internal/policy/protected.go`
- Test: `internal/policy/glob_test.go`, `internal/policy/protected_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces (exact signatures; later tasks call these):

```go
// ValidatePattern reports whether pattern is a usable protected-path pattern.
func ValidatePattern(pattern string) error

// MatchPath reports whether the repo-relative, slash-separated path is
// covered by pattern. Call ValidatePattern first; an invalid pattern never matches.
func MatchPath(pattern, path string) bool

// IsProtected reports whether path is covered by an always-protected pattern
// or by one of extra, and returns the pattern that matched ("" if none).
func IsProtected(path string, extra []string) (pattern string, protected bool)

// AlwaysProtected returns a copy of the hardcoded patterns, for display.
func AlwaysProtected() []string
```

**Pattern semantics (gitignore-style, the complete definition):**

1. Paths are repo-relative with `/` separators and no leading `./` or `/`. A path is split into segments on `/`.
2. A leading `/` on a pattern anchors it to the repo root and is then removed. A trailing `/` is removed.
3. A pattern with no `/` left (after rule 2, and not originally anchored) matches if **any single segment** of the path matches it. So `tsconfig.json` covers `tsconfig.json` and `packages/app/tsconfig.json`, and `node_modules` covers `a/node_modules/x/y.js`.
4. Any other pattern is anchored at the root and split into segments. A `**` segment matches zero or more path segments. Every other segment is matched against one path segment with `path.Match` (`*`, `?`, `[...]`; `*` never crosses `/`).
5. An anchored pattern covers a path if it matches the whole path **or any leading directory prefix of it**. So `.github` and `.github/**` both cover `.github/workflows/ci.yml`; `docs/api` covers `docs/api/v1/x.md`.
6. Matching is case-sensitive in `MatchPath`.

`ValidatePattern` rejects, each with a clear message: the empty string or only whitespace; a pattern starting with `!` (negation is not supported: a pattern can add protection, never remove it); a pattern containing a backslash, a NUL byte, or a newline; a segment equal to `.` or `..`; a segment where `**` is mixed with other characters (such as `a**b`); a segment that `path.Match` reports as malformed (`path.ErrBadPattern`); a pattern that is only `/`.

**Always-protected patterns** (in `protected.go`, unexported slice, matched **case-insensitively** by lower-casing both pattern and path; `extra` patterns are matched case-sensitively):

```
regreen.yml
regreen.yaml
.regreen
.github
.git
.githooks
.husky
.pre-commit-config.yaml
lefthook.yml
.lefthook.yml
```

All are no-slash patterns, so by rule 3 they cover that name at any depth and everything beneath it. `IsProtected` checks the hardcoded list first, then `extra` in order, and skips any `extra` entry that fails `ValidatePattern`.

- [ ] **Step 1: Write the failing tests**

`glob_test.go`, `TestMatchPath` table (pattern, path, want). Include at least these rows:

| pattern | path | want |
|---|---|---|
| `tsconfig.json` | `tsconfig.json` | true |
| `tsconfig.json` | `packages/app/tsconfig.json` | true |
| `tsconfig.json` | `tsconfig.json.bak` | false |
| `/tsconfig.json` | `tsconfig.json` | true |
| `/tsconfig.json` | `packages/app/tsconfig.json` | false |
| `.eslintrc*` | `.eslintrc` | true |
| `.eslintrc*` | `.eslintrc.json` | true |
| `.eslintrc*` | `web/.eslintrc.cjs` | true |
| `.eslintrc*` | `eslintrc.json` | false |
| `.github/**` | `.github/workflows/ci.yml` | true |
| `.github/**` | `.github` | true |
| `.github/**` | `x/.github/workflows/ci.yml` | false |
| `.github` | `.github/workflows/ci.yml` | true |
| `.github` | `x/.github/workflows/ci.yml` | true |
| `docs/api` | `docs/api/v1/x.md` | true |
| `docs/api` | `docs/apiary.md` | false |
| `src/**/gen/*.go` | `src/gen/a.go` | true |
| `src/**/gen/*.go` | `src/a/b/gen/a.go` | true |
| `src/**/gen/*.go` | `src/a/b/gen/sub/a.go` | false |
| `**/secret.txt` | `secret.txt` | true |
| `**/secret.txt` | `a/b/secret.txt` | true |
| `*.lock` | `yarn.lock` | true |
| `*.lock` | `a/b/c.lock` | true |
| `a/*.go` | `a/b/c.go` | false |
| `node_modules` | `a/node_modules/x/y.js` | true |
| `Tsconfig.json` | `tsconfig.json` | false |
| `dir/` | `dir/file` | true |
| `a[` | `a[` | false (invalid pattern never matches) |

`TestValidatePattern` table: one valid row for each of `tsconfig.json`, `/a/b`, `.github/**`, `src/**/x`, `*.lock`, `a/[abc]?.go`; one invalid row for each rejection listed above (`""`, `"  "`, `!regreen.yml`, `a\b`, `a\x00b`, `"a\nb"`, `a/../b`, `./a`, `a**b`, `a[`, `/`), asserting a non-nil error.

`protected_test.go`:
- `TestAlwaysProtected` table (path, want protected): `regreen.yml`, `regreen.yaml`, `REGREEN.YML`, `sub/regreen.yml`, `.github/workflows/ci.yml`, `.GitHub/workflows/ci.yml`, `.github`, `.regreen/state.json`, `.git/hooks/pre-push`, `.githooks/pre-push`, `.husky/pre-commit`, `.pre-commit-config.yaml`, `lefthook.yml`, `.lefthook.yml` are all protected with `extra == nil`; `src/main.go`, `regreen.yml.md`, `github/x`, `docs/regreen.md` are not.
- `TestIsProtectedExtra`: `extra = []string{"tsconfig.json", "!regreen.yml", ".eslintrc*"}`: `packages/a/tsconfig.json` protected with pattern `tsconfig.json`; `.eslintrc.json` protected; `regreen.yml` still protected (the `!` entry is ignored and removes nothing); `src/a.ts` not protected. Also assert `extra` matching is case-sensitive (`TSCONFIG.JSON` is not protected by extra `tsconfig.json`).
- `TestAlwaysProtectedReturnsCopy`: mutating the returned slice does not change a second call's result or `IsProtected("regreen.yml", nil)`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/policy/`
Expected: compile failure, `undefined: MatchPath` and the other three names.

- [ ] **Step 3: Implement `glob.go` and `protected.go`** to the semantics above. Keep `glob.go` to pure string and `path` package operations. The `**` matcher is a small recursive or iterative function over two segment slices.

- [ ] **Step 4: Run tests and the full check**

Run: `go test ./internal/policy/ && make check`
Expected: PASS, `0 issues`.

- [ ] **Step 5: Commit**

```bash
git add internal/policy/glob.go internal/policy/glob_test.go
git commit -m "feat(policy): add gitignore-style path matcher"
git add internal/policy/protected.go internal/policy/protected_test.go
git commit -m "feat(policy): hardcode always-protected paths"
```

---

### Task 2: Diff model and unified-diff parser

**Files:**
- Create: `internal/policy/diff.go`, `internal/policy/parse.go`
- Test: `internal/policy/parse_test.go`

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces:

```go
type ChangeKind int

const (
	Modified ChangeKind = iota
	Added
	Deleted
)

func (k ChangeKind) String() string // "modified", "added", "deleted"

// Line is one line added by a diff.
type Line struct {
	Number int    // line number in the new version of the file, starting at 1
	Text   string // content without the leading '+' and without the line ending
}

// FileChange is the change to one file.
type FileChange struct {
	Path    string     // repo-relative, slash-separated
	Kind    ChangeKind
	Added   []Line     // lines added, in order
	Deleted int        // number of lines removed
	Binary  bool       // git reported a binary change; Added/Deleted are empty
	Special string     // "", or one of "mode change", "symlink", "submodule"
}

// Diff is a set of file changes, in the order the patch lists them.
type Diff struct {
	Files []FileChange
}

func (d Diff) AddedLines() int   // sum of len(Added)
func (d Diff) DeletedLines() int // sum of Deleted

// ParseUnifiedDiff parses the output of
//   git -c core.quotepath=off diff --no-color --no-ext-diff --no-textconv --no-renames <base> <head>
// It returns an error for anything it does not recognize.
func ParseUnifiedDiff(patch string) (Diff, error)
```

**Parser rules (complete):**

1. Empty input (or only whitespace) is an empty `Diff` and no error.
2. Input is a sequence of file sections, each starting with a line `diff --git <old> <new>`. Any non-empty line before the first section is an error.
3. Header lines allowed between `diff --git` and the first hunk, in any order: `index ...`, `new file mode <mode>` (Kind = Added), `deleted file mode <mode>` (Kind = Deleted), `old mode <mode>` and `new mode <mode>` (Special = "mode change"), `--- <path>`, `+++ <path>`, `Binary files ... differ` (Binary = true), `GIT binary patch` (Binary = true; skip that section's following lines up to the next `diff --git` line or end of input). `similarity index`, `rename from`, `rename to`, `copy from`, `copy to`, and any other unrecognized header line are errors (the caller must pass `--no-renames`).
4. A mode of `120000` on any mode line sets Special = "symlink"; `160000` sets Special = "submodule". These take precedence over "mode change".
5. Path: from `+++ b/<path>` when present and not `/dev/null`; otherwise from `--- a/<path>`; otherwise (no `---`/`+++` lines: binary, mode-only, or empty file) from the `diff --git` line. With `--no-renames` the two names on that line are the same path `P`, written `a/P b/P`; recover `P` by length (the remainder is `2 + len(P) + 3 + len(P)` bytes) and verify both halves agree, else error. Either name may be C-quoted (`"a/caf\303\251.txt"`): a name starting with `"` is unquoted with git's C-style rules (`\\`, `\"`, `\n`, `\t`, `\a`, `\b`, `\f`, `\r`, `\v`, and three-digit octal `\NNN`), and a malformed quoted name is an error. When both `---`/`+++` and the `diff --git` name are available they must agree, else error. A `---` or `+++` path that is not `/dev/null` and lacks the `a/` or `b/` prefix is an error.
6. The final path must be non-empty, must not start with `/`, must equal `path.Clean` of itself, must not be `..` or start with `../`, and must not contain a NUL byte or a backslash; otherwise error. Two sections with the same path are an error.
7. Hunks: a line `@@ -<a>[,<b>] +<c>[,<d>] @@...` (omitted count means 1). Then read exactly `b` old-side lines and `d` new-side lines by their first byte: `' '` counts for both sides, `'-'` old side (Deleted++), `'+'` new side (append `Line{Number, Text}`, with Number starting at `c` and advancing on each `' '` and `'+'` line). A line starting with `\` (`\ No newline at end of file`) is skipped and counts for neither side. Counting by the hunk header, not by scanning for the next header, is required: an added line whose content is `diff --git a/x b/x` or `@@ -1 +1 @@` is content. Any other first byte inside a hunk, or input ending before the counts are satisfied, is an error. A malformed hunk header is an error.
8. Line endings: split the patch on `\n`. A trailing `\r` on an added line's content is kept in `Text` as is (do not strip; a pattern check must see what was added). Header lines are matched after trimming one trailing `\r`.
9. Kind defaults to Modified. If `--- /dev/null` appears, Kind must be Added; if `+++ /dev/null` appears, Kind must be Deleted; a contradiction with the mode lines is an error.
10. Errors name the 1-based patch line number: `fmt.Errorf("parse diff: line %d: ...", n)`.

- [ ] **Step 1: Write the failing tests** in `parse_test.go`. Each test case embeds a literal patch string and the expected `Diff` (compare with `reflect.DeepEqual`), or expects an error. Required cases:

Success cases:
1. Empty string, and `"\n"`: empty Diff.
2. One modified file, one hunk with context: `-` one line, `+` two lines; check `Added` numbers and text, `Deleted == 1`.
3. `-U0` style hunk headers with omitted counts (`@@ -3 +3 @@`).
4. Two hunks in one file: line numbers continue from each hunk's `+c`.
5. New file (`new file mode 100644`, `--- /dev/null`, `+++ b/new.go`): Kind Added, lines numbered from 1.
6. Deleted file (`deleted file mode 100644`, `+++ /dev/null`): Kind Deleted, `Deleted` equals the hunk's old count, no Added.
7. Two files in one patch, order preserved.
8. `\ No newline at end of file` after a `+` line and after a `-` line.
9. Binary: `Binary files a/img.png and b/img.png differ` with no `---`/`+++`: Binary true, path `img.png`.
10. `GIT binary patch` followed by base85-looking lines, then a second normal file section: first Binary, second parsed normally.
11. Mode-only change (`old mode 100644` / `new mode 100755`, no hunks): Special "mode change", path from the `diff --git` line.
12. New symlink (`new file mode 120000`): Special "symlink". Submodule (`new file mode 160000`): Special "submodule".
13. Path with spaces: `diff --git a/my file.txt b/my file.txt` with `--- a/my file.txt` / `+++ b/my file.txt`.
14. Mode-only change on a path with spaces (path recovered by length from the `diff --git` line).
15. C-quoted path: `diff --git "a/tab\there.txt" "b/tab\there.txt"` with quoted `---`/`+++`: path contains a real tab. And an octal case `"a/caf\303\251.txt"`: path `café.txt`.
16. Added line whose content is `diff --git a/x b/x`, and one whose content is `@@ -1 +1 @@`, and one whose content starts with `--- a/`: all are content of the current file; the Diff has one file.
17. Added line with trailing `\r`: `Text` ends with `\r`.
18. New empty file (`new file mode 100644`, `index 0000000..e69de29`, no hunks): Kind Added, no lines.
19. `AddedLines()` / `DeletedLines()` totals across two files.

Error cases (each must return a non-nil error, and the message must contain `line `):
20. Text before the first `diff --git`.
21. `rename from a` / `rename to b` headers. `similarity index 90%`. `copy from`.
22. An unknown header line (`frobnicate yes`).
23. Hunk shorter than its header counts (input ends early).
24. A hunk line starting with an unexpected byte (for example `x`).
25. Malformed hunk header (`@@ -a,b +c @@`, `@@ nonsense`).
26. `+++ b/../outside.txt`; `+++ b//abs`; `+++ outside.txt` (missing `b/`); a path containing a backslash.
27. `diff --git a/x b/y` with no `---`/`+++` (halves disagree).
28. `diff --git a/x b/x` whose `+++` says `b/other`.
29. The same path in two sections.
30. `--- /dev/null` without `new file mode` contradiction: `deleted file mode 100644` together with `--- /dev/null`.
31. Malformed C-quoted name (`"a/bad\9"`, unterminated quote).

Also add a native fuzz test:

```go
func FuzzParseUnifiedDiff(f *testing.F) {
	f.Add("diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\n")
	f.Add("")
	f.Fuzz(func(t *testing.T, patch string) {
		d, err := ParseUnifiedDiff(patch)
		if err != nil {
			return
		}
		for _, fc := range d.Files {
			if fc.Path == "" || strings.HasPrefix(fc.Path, "/") || fc.Path != path.Clean(fc.Path) || strings.HasPrefix(fc.Path, "../") || fc.Path == ".." {
				t.Fatalf("accepted unsafe path %q", fc.Path)
			}
			if fc.Deleted < 0 {
				t.Fatalf("negative deleted count")
			}
		}
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/policy/`
Expected: compile failure, `undefined: ParseUnifiedDiff`.

- [ ] **Step 3: Implement `diff.go` and `parse.go`** to the rules above. Structure the parser as a small type holding the lines and a cursor, with one method per section part (header, hunk). No regular expressions are needed; if you use one, compile it once at package level.

- [ ] **Step 4: Run tests, a short fuzz run, and the full check**

Run: `go test ./internal/policy/ && go test ./internal/policy/ -run '^$' -fuzz FuzzParseUnifiedDiff -fuzztime 30s && make check`
Expected: PASS; fuzzing reports no failures (if it finds one, fix the parser, keep the generated corpus file under `internal/policy/testdata/fuzz/` as a regression case, and say so in the report); `0 issues`.

- [ ] **Step 5: Commit**

```bash
git add internal/policy/diff.go internal/policy/parse.go internal/policy/parse_test.go
git commit -m "feat(policy): add diff model and fail-closed unified diff parser"
```
(Include `internal/policy/testdata/` if fuzzing produced regression inputs.)

---

### Task 3: Policy evaluation

**Files:**
- Create: `internal/policy/policy.go`
- Test: `internal/policy/policy_test.go`, `internal/policy/hardening_test.go`

**Interfaces:**
- Consumes: `IsProtected(path string, extra []string) (string, bool)` from Task 1; `Diff`, `FileChange`, `Line`, `ChangeKind` and `ParseUnifiedDiff` from Task 2.
- Produces:

```go
type EditScope string

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
func DefaultRules() Rules

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
	RuleProtectedFile    = "protected_file"
	RuleEditScope        = "edit_scope"
	RuleBinaryFile       = "binary_file"
	RuleSpecialFile      = "special_file"
	RuleForbiddenPattern = "forbidden_pattern"
	RuleMaxChangedLines  = "max_changed_lines"
	RuleMaxDeletedLines  = "max_deleted_lines"
)

type Reason struct {
	Rule    string // one of the Rule* constants
	File    string // "" when the reason is not about one file
	Line    int    // new-file line number, 0 when not about one line
	Message string // one sentence a fixing agent can act on
}

type Verdict struct {
	Allowed           bool
	Reasons           []Reason // empty exactly when Allowed is true
	AttemptsRemaining int      // max(0, MaxAttempts-AttemptsUsed), independent of Allowed
}

func Evaluate(in Input) Verdict
```

**Evaluation rules (complete). All checks run; reasons accumulate in this order:**

1. **Rules sanity (fail closed).** If `MaxChangedLines < 1`, `MaxDeletedLines < 0`, `MaxAttempts < 1`, `EditScope` is not one of the two constants, or any `ForbiddenPatterns` entry is the empty string: one `invalid_rules` reason per problem, and no further rule is evaluated. (Config validation prevents this; the engine still refuses to run on rules it cannot interpret.)
2. **Attempts.** If `AttemptsUsed >= MaxAttempts`: reason `max_attempts`, message `no attempts remaining (used N of M); stop and report to the developer`. A negative `AttemptsUsed` is treated as `invalid_rules` (rule 1).
3. **Empty diff.** If the diff has no files: reason `empty_diff`.
4. **Per file, in diff order:**
   - `protected_file` if `IsProtected(file.Path, Rules.ProtectedFiles)`. Message names the path and the matching pattern, and says whether it is always protected or protected by the project config. Applies to added, modified, and deleted files alike.
   - `edit_scope` if `EditScope == ScopeFilesInErrors` and the path is not in `ErrorFiles`. Compare after `path.Clean` on each `ErrorFiles` entry (with a leading `./` removed); comparison is exact and case-sensitive. An empty `ErrorFiles` therefore rejects every file. Message: `<path> is not named in the errors; only these files may be edited: <sorted list>` (or `no files are named in the errors` when empty).
   - `binary_file` if `Binary`.
   - `special_file` if `Special != ""`; message includes the Special value.
   - `forbidden_pattern` for each added line and each pattern where `strings.Contains(line.Text, pattern)`: `File`, `Line` set; message `added line contains forbidden pattern "<pattern>"`. One reason per (line, pattern) pair. Deleted and context lines are never checked: removing a forbidden pattern must be allowed.
5. **Totals.**
   - `max_deleted_lines` if `Diff.DeletedLines() > MaxDeletedLines`; message gives both numbers.
   - `max_changed_lines` if `Diff.AddedLines()+Diff.DeletedLines() > MaxChangedLines`; message gives both numbers.
6. **Reason cap.** At most 20 `forbidden_pattern` reasons are returned; if there were more, append one extra `forbidden_pattern` reason with `File == ""`, `Line == 0`, and message `and N more added lines contain forbidden patterns`. No other rule is capped.
7. `Allowed = len(Reasons) == 0`. `Evaluate` never panics, never mutates its input, and returns the same result for the same input.

- [ ] **Step 1: Write the failing tests**

`policy_test.go`, `TestEvaluate`, a table where each case builds an `Input` from `DefaultRules()` plus overrides and a `Diff` literal, and asserts `Allowed`, the ordered list of `Reason.Rule` values, and for selected reasons the `File` and `Line`. Use a small helper `mod(path string, deleted int, added ...string) FileChange` that numbers added lines from 1. Required cases:

1. One file in `ErrorFiles`, small change: allowed, no reasons, `AttemptsRemaining == 2`.
2. File not in `ErrorFiles`: `edit_scope`.
3. `ErrorFiles` empty with default scope: `edit_scope` for every file.
4. `EditScope = ScopeAny` and file not in `ErrorFiles`: allowed.
5. `ErrorFiles` entry `./src/a.ts` matches diff path `src/a.ts`.
6. `ErrorFiles` entry differing only in case: `edit_scope`.
7. Diff touches `regreen.yml` (in `ErrorFiles`, scope any): `protected_file`, message mentions "always protected".
8. Diff touches `.github/workflows/ci.yml`: `protected_file`.
9. Diff deletes `.github/workflows/ci.yml` (Kind Deleted): `protected_file`.
10. Diff touches `packages/a/tsconfig.json` with `ProtectedFiles = []string{"tsconfig.json"}`: `protected_file`, message mentions the project config.
11. `ProtectedFiles = []string{"!regreen.yml"}` and diff touches `regreen.yml`: still `protected_file`.
12. Added line containing `eslint-disable` with that pattern configured: `forbidden_pattern` with correct `File` and `Line`.
13. Deleted-only removal where the removed text contained the pattern (represented as `Deleted: 1`, no added lines): allowed.
14. Pattern matching is case-sensitive: added `ESLINT-DISABLE` with pattern `eslint-disable`: allowed.
15. One added line matching two patterns: two reasons.
16. 25 added lines each matching: 20 reasons plus one summary reason whose message contains `5 more`.
17. Deleted lines 51 with default rules: `max_deleted_lines`. Exactly 50: allowed.
18. Added 150 + deleted 51 over two files: both `max_deleted_lines` and `max_changed_lines`, in that order. Added 150 + deleted 50: allowed (200 is the limit, inclusive).
19. `MaxDeletedLines = 0` with one deletion: `max_deleted_lines`; with none: allowed.
20. Binary file in scope: `binary_file`. Special "symlink": `special_file`, message contains `symlink`.
21. Empty diff: `empty_diff`.
22. `AttemptsUsed = 2`, otherwise fine: not allowed, `max_attempts`, `AttemptsRemaining == 0`. `AttemptsUsed = 1`: allowed, `AttemptsRemaining == 1`. `AttemptsUsed = 5`: `AttemptsRemaining == 0`.
23. Invalid rules, one case each: `MaxChangedLines = 0`, `MaxDeletedLines = -1`, `MaxAttempts = 0`, `EditScope = "everything"`, `EditScope = ""`, `ForbiddenPatterns = []string{""}`, `AttemptsUsed = -1`: only `invalid_rules` reasons, even when the diff also touches a protected file.
24. Several violations in one diff (protected file + out of scope file + forbidden pattern + over the deletion limit): all reported, in the documented order.
25. Purity: call `Evaluate` twice on the same `Input` and compare verdicts with `reflect.DeepEqual`; deep-copy the input first and assert it is unchanged afterwards.

`hardening_test.go`, `TestHardeningScenarios`: one subtest per row of spec section 6 that needs no network, each named after the scenario, each starting from a **real patch string** passed through `ParseUnifiedDiff` and then `Evaluate`:

- "agent edits regreen.yml to relax rules": patch raising `max_deleted_lines` in `regreen.yml` is rejected with `protected_file`, even with `EditScope: ScopeAny` and `regreen.yml` listed in `ErrorFiles`.
- "agent removes the verify job from the workflow": patch deleting lines from `.github/workflows/ci.yml` is rejected with `protected_file`; a patch deleting the whole workflow file is too.
- "agent resets the attempt counter by deleting state": `Evaluate` has no state input; with `AttemptsUsed` (as counted from history) equal to `MaxAttempts` the verdict is `max_attempts` regardless of anything else in the input. Also assert with `reflect` that `Input` has exactly the four fields `Diff`, `Rules`, `ErrorFiles`, `AttemptsUsed`, so adding an agent-controlled input later fails this test.
- "agent passes lint by deleting code": patch removing 60 lines from the failing file: `max_deleted_lines`.
- "agent passes lint by commenting out code": patch turning 60 lines into `// ...` comments (60 `-` and 60 `+`): `max_deleted_lines`.
- "agent passes lint with a suppression comment": patch adding `// eslint-disable-next-line` and one adding `// @ts-ignore`: `forbidden_pattern` at the right line.
- "agent edits a file the errors do not name": `edit_scope`.
- "instructions injected through CI logs": the errors text is irrelevant to `Evaluate`; a diff that does what an injected instruction asked (adds a step to `.github/workflows/ci.yml`, and edits `.husky/pre-push`) is rejected with `protected_file` for both files.
- "an honest one-line fix passes": a one-line change to the file named in the errors is allowed.
- "unparseable diff is not evaluated": a patch with a rename header makes `ParseUnifiedDiff` return an error (the caller must treat that as a rejection).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/policy/`
Expected: compile failure, `undefined: Evaluate`.

- [ ] **Step 3: Implement `policy.go`** to the rules above.

- [ ] **Step 4: Enforce purity with a test, then run the full check**

Add to `hardening_test.go` a test `TestPolicyPackageIsPure` that parses the package's non-test `.go` files with `go/parser` (`parser.ImportsOnly`) from the test's working directory and fails if any import path has a `.` in its first element (third-party), or is one of `os`, `os/exec`, `io/fs`, `io/ioutil`, `net`, `net/http`, `time`, `syscall`, `unsafe`, or starts with `github.com/Tawfiq-alqaidy/regreen-ci/`. Test files may use `os` to list the sources; only non-`_test.go` files are checked. Prove the test works: temporarily add `import _ "os"` to `policy.go`, see it fail, revert, and record both outputs in your report.

Run: `go test ./internal/policy/ && make check`
Expected: PASS, `0 issues`.

- [ ] **Step 5: Commit**

```bash
git add internal/policy/policy.go internal/policy/policy_test.go
git commit -m "feat(policy): evaluate diffs against rules with actionable reasons"
git add internal/policy/hardening_test.go
git commit -m "test(policy): cover the offline hardening scenarios from the brief"
```

---

### Task 4: Repo config and user config

**Files:**
- Create: `internal/config/repo.go`, `internal/config/user.go`
- Test: `internal/config/repo_test.go`, `internal/config/user_test.go`
- Modify: `go.mod`, `go.sum` (the YAML dependency)

**Interfaces:**
- Consumes from `internal/policy`: `Rules`, `DefaultRules()`, `EditScope`, `ScopeFilesInErrors`, `ScopeAny`, `ValidatePattern(pattern string) error`.
- Produces:

```go
// RepoFileName is the name of the repo config file at the repository root.
const RepoFileName = "regreen.yml"

type Check struct {
	Name string // unique within the file
	Run  string // command that performs the check
	Fix  string // optional deterministic fix command; "" when absent
}

type Repo struct {
	Checks []Check
	Policy policy.Rules // defaults applied, validated
}

// ParseRepo parses and validates the contents of a regreen.yml.
// Callers that must not trust the working tree (regreen verify) pass bytes
// read from the base branch.
func ParseRepo(data []byte) (*Repo, error)

// LoadRepo reads path and calls ParseRepo.
func LoadRepo(ctx context.Context, path string) (*Repo, error)

type Delivery string

const (
	DeliveryNotify   Delivery = "notify"   // default
	DeliverySession  Delivery = "session"  // push to a running agent session
	DeliveryHeadless Delivery = "headless" // start a new headless run
)

type User struct {
	Agent string // "", "claude-code", "codex", or "other"
	Watch struct {
		Delivery Delivery
	}
}

func DefaultUser() User // Agent "", Watch.Delivery "notify"

// UserConfigPath returns <os.UserConfigDir()>/regreen/config.yml.
func UserConfigPath() (string, error)

// LoadUser reads the user config at path. A missing file is not an error:
// it returns DefaultUser().
func LoadUser(ctx context.Context, path string) (*User, error)
```

**`regreen.yml` schema (the only accepted keys):**

```yaml
checks:                      # required, at least one
  - name: lint               # required, unique
    run: npm run lint        # required
    fix: npx eslint --fix .  # optional
policy:                      # optional; every key optional
  protected_files: [".eslintrc*", "tsconfig.json"]
  forbidden_patterns: ["eslint-disable", "@ts-ignore", "@ts-expect-error"]
  max_changed_lines: 200
  max_deleted_lines: 50
  max_attempts: 2
  edit_scope: files_in_errors   # or: any
```

**Parsing and validation rules:**

1. Look up the library first: `go list -m -versions go.yaml.in/yaml/v3` and `go list -m -versions go.yaml.in/yaml/v4`; read the project README at https://github.com/yaml/go-yaml for which major version is the current stable release line. Use the newest **released, non-pre-release** version of the recommended stable major, add it with `go get <module>@<version>`, and record module, version, and the reason in your report. Confirm from the package docs (`go doc <module>.Decoder`) that strict decoding (`KnownFields(true)`) exists; if the API differs from this description, use the documented equivalent and say so. If neither major has a stable release, stop and report BLOCKED.
2. Input larger than 1 MiB (1048576 bytes): error, before parsing.
3. Decode with unknown fields rejected. Exactly one YAML document: a second document is an error; empty input or a document that is null is an error (`regreen.yml is empty`).
4. Use unexported raw structs with pointer fields for the numeric and scope keys so that "omitted" is distinguishable from an explicit `0`. Omitted keys take `policy.DefaultRules()` values.
5. Collect **all** validation problems and return them together with `errors.Join`, each prefixed with a field path. Rules:
   - `checks`: at least one entry.
   - `checks[i].name`: non-empty after trimming; matches `^[A-Za-z0-9][A-Za-z0-9._-]*$`; unique (case-sensitive).
   - `checks[i].run`: non-empty after trimming. `checks[i].fix`: if present, non-empty after trimming.
   - `run` and `fix` must not contain a NUL byte.
   - `policy.protected_files[i]`: must pass `policy.ValidatePattern` (so `!pattern` is rejected: config can add protection, never remove it).
   - `policy.forbidden_patterns[i]`: non-empty; must not contain a newline.
   - `policy.max_changed_lines >= 1`; `policy.max_deleted_lines >= 0`; `policy.max_attempts >= 1`; each `<= 1000000`.
   - `policy.edit_scope`: `files_in_errors` or `any`.
6. `LoadRepo`: return `ctx.Err()` if the context is already done; wrap read errors as `read <path>: %w` (so `errors.Is(err, fs.ErrNotExist)` works); wrap parse errors as `<path>: %w`.
7. `ParseRepo` never executes, expands, or interprets `run`/`fix`; they are stored as written.

**User config (`config.yml`) schema:**

```yaml
agent: claude-code     # optional: claude-code | codex | other
watch:
  delivery: notify     # optional: notify | session | headless
```

Same strictness: unknown keys rejected, one document, 1 MiB cap. An empty file is valid and yields defaults. An unknown `agent` or `delivery` value is an error naming the allowed values. `UserConfigPath` wraps the `os.UserConfigDir` error.

- [ ] **Step 1: Write the failing tests**

`repo_test.go`:
- `TestParseRepoFull`: the brief's example file parses into the expected `Repo` (compare with `reflect.DeepEqual`).
- `TestParseRepoDefaults`: a file with only one check and no `policy` key yields `policy.DefaultRules()`; a file with `policy: {max_deleted_lines: 0}` yields `MaxDeletedLines == 0` and defaults elsewhere; `policy: {edit_scope: any}` yields `ScopeAny`.
- `TestParseRepoInvalid` table (yaml, substring the error must contain): empty input; `null`; no `checks` key; `checks: []`; missing `name`; missing `run`; `fix: ""`; duplicate names; name with a space; unknown top-level key (`cheks:`); unknown key inside a check (`cmd:`); unknown policy key (`max_lines:`); `max_changed_lines: 0`; `max_deleted_lines: -1`; `max_attempts: 0`; `max_attempts: 9999999`; `edit_scope: everything`; `protected_files: ["!regreen.yml"]`; `protected_files: [""]`; `forbidden_patterns: [""]`; a second YAML document (`---` separator); a non-mapping document (`- a`); `max_attempts: "two"`; malformed YAML (`checks: [`).
- `TestParseRepoReportsAllProblems`: a file with three distinct problems returns an error whose message contains all three field paths.
- `TestParseRepoTooLarge`: 1 MiB + 1 byte of input is rejected with a size message.
- `TestParseRepoDoesNotRemoveHardcodedProtection`: for a parsed config with `protected_files: ["src/gen"]`, `policy.IsProtected("regreen.yml", repo.Policy.ProtectedFiles)` and `policy.IsProtected(".github/workflows/ci.yml", ...)` are still true.
- `TestLoadRepo`: writes a file under `t.TempDir()` and loads it; a missing path returns an error for which `errors.Is(err, fs.ErrNotExist)` is true and whose message contains the path; a cancelled context returns `context.Canceled`.

`user_test.go`:
- `TestLoadUserMissingFileIsDefaults`, `TestLoadUserEmptyFileIsDefaults`.
- `TestLoadUser` table: full file; only `agent`; only `watch.delivery`; each allowed value.
- `TestLoadUserInvalid` table: unknown key; `agent: gemini`; `delivery: email`; second document; malformed YAML; oversized.
- `TestUserConfigPath`: with `t.Setenv("XDG_CONFIG_HOME", dir)` and `t.Setenv("HOME", dir)`, the result ends with `filepath.Join("regreen", "config.yml")` and is absolute. (On macOS `os.UserConfigDir` uses `$HOME/Library/Application Support`; assert only the suffix and absoluteness.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/`
Expected: compile failure (`undefined: ParseRepo` and others).

- [ ] **Step 3: Add the dependency and implement `repo.go` and `user.go`.** Share the strict single-document decode in one unexported helper used by both files.

- [ ] **Step 4: Run tests and the full check**

Run: `go test ./internal/config/ && go mod tidy && git diff --exit-code go.mod go.sum; make check`
Expected: PASS; `go mod tidy` leaves `go.mod`/`go.sum` unchanged; `go.mod` has exactly one `require` line for the YAML module (plus none indirect, or only what that module itself needs); `0 issues`.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add the YAML library dependency"
git add internal/config/repo.go internal/config/repo_test.go
git commit -m "feat(config): load and validate regreen.yml with strict defaults"
git add internal/config/user.go internal/config/user_test.go
git commit -m "feat(config): load the user config from the OS config directory"
```

---

### Task 5: Documentation and milestone summary

**Files:**
- Modify: `docs/ARCHITECTURE.md`, `docs/DECISIONS.md`, `README.md`
- Create: `docs/milestones/M1.md`, `docs/CONFIG.md`

**Interfaces:**
- Consumes: everything Tasks 1 to 4 built. Read the code before writing; every statement must be true of the code as committed.
- Produces: documentation only.

Writing rules: plain language, no marketing, no emoji. State only what exists; label everything else with its milestone. Do not rewrite decisions D1 to D10; add new entries below them.

- [ ] **Step 1: `docs/ARCHITECTURE.md`.** Extend "What exists today" (retitle it to cover M0 and M1) with `internal/policy` and `internal/config`: each file and its responsibility, the data flow `regreen.yml bytes -> config.ParseRepo -> policy.Rules`, `patch text -> policy.ParseUnifiedDiff -> policy.Diff`, `policy.Evaluate(Input) -> Verdict`, the order in which `Evaluate` applies its rules, and the fact that nothing calls these packages from the CLI yet. Update the "Planned layout" table so M1 packages are marked as built. In the trust model section, mark what M1 delivered (always-protected paths, the policy engine) and leave the rest labelled planned.

- [ ] **Step 2: `docs/DECISIONS.md`.** Append entries, same format as the existing ones (Date 2026-11-23 (M1), Decision, Alternatives, Why, and "Cost if wrong" where it applies):
  - D11. YAML library: the exact module and version from `go.mod`; alternatives `goccy/go-yaml` and the archived `gopkg.in/yaml.v3`; why (maintained, same widely known API, strict decoding); this is the project's first dependency.
  - D12. Strict config: unknown keys rejected, one document, 1 MiB cap, all problems reported at once.
  - D13. Strict defaults for omitted policy keys (200 / 50 / 2 / `files_in_errors`); alternatives: all keys required, or unlimited when omitted. Chosen by the maintainer.
  - D14. `forbidden_patterns` are literal, case-sensitive substrings checked only against added lines; alternatives: regular expressions, opt-in `re:` prefix. Chosen by the maintainer. Note the consequence: removing a suppression comment is always allowed.
  - D15. gitignore-style `protected_files` patterns with an in-repo matcher (no dependency); a matched directory covers everything under it; negation (`!`) is rejected so config can only add protection. Chosen by the maintainer.
  - D16. The always-protected list and its interpretation of the brief's "git hooks": list every hardcoded pattern; say they match case-insensitively at any depth; say that `.githooks`, `.husky`, `.pre-commit-config.yaml`, `lefthook.yml`, `.lefthook.yml` are this project's reading of "git hooks" and can be extended.
  - D17. The diff parser lives in `internal/policy` and is built in M1 (the brief lists git operations under `internal/vcs` in M3): it is a pure function, the hardening tests need real patches, and a parser the policy does not own is a bypass risk. It requires `--no-renames` input and rejects anything it does not recognize.
  - D18. Fail-closed handling of binary files, symlinks, submodules, and mode changes: any of them rejects the fix.
  - D19. Attempts are an input number (`AttemptsUsed`), not state: the policy engine cannot read or be fooled by a state file; counting from commit trailers arrives in M3.
  - D20. User config shape (`agent`, `watch.delivery`) is provisional until M6 uses it.

- [ ] **Step 3: `docs/CONFIG.md`.** A reference for `regreen.yml`: every key, its type, default, and validation rule; the pattern syntax for `protected_files` with examples (take them from the `TestMatchPath` table); the always-protected list; what each rejection reason (`Rule*` constant) means; the user config file, its location per OS (as `os.UserConfigDir` documents), and its keys. Say plainly that M1 only loads and evaluates; no command reads these files yet.

- [ ] **Step 4: `README.md`.** Mark M1 in the status table as done-pending-review in the same style M0 uses, update the "Status" paragraph, and link `docs/CONFIG.md` and `docs/milestones/M1.md` from the Documentation list.

- [ ] **Step 5: `docs/milestones/M1.md`.** The plain-language end-of-milestone summary, same sections as `docs/milestones/M0.md`: What was built; How the pieces connect; How to try it by hand (there is no command yet, so give exact `go test` invocations, e.g. `go test ./internal/policy/ -run TestHardeningScenarios -v`, with the output to expect, verified by running them; and a short Go snippet a reader could paste into a scratch test to call `config.ParseRepo` and `policy.Evaluate`, also verified by running it in a temporary test file that you then delete); Go idioms used and why (pure package with injected inputs; table-driven tests and subtests; `errors.Join` and `%w` wrapping with `errors.Is`; pointer fields to tell "omitted" from zero; returning a copy of a package-level slice; typed string constants as enums; native fuzzing; `go/parser` in a test to enforce an import rule; `context.Context` as first parameter); a table mapping each row of spec section 6 to the test that covers it or the milestone that will; What is deliberately not here yet; Known limitations (at least: literal patterns can be evaded by rewording, e.g. a linter's alternative suppression syntax, which is why human review remains part of the design; `edit_scope` trusts the list of files in the errors, which M2 will produce; the parser requires `--no-renames`).

- [ ] **Step 6: Verify and commit**

Run: `make check && git status --short`
Expected: check passes; only the five documentation files changed. Check every command and code snippet in `M1.md` and `CONFIG.md` by running it.

```bash
git add docs/ARCHITECTURE.md docs/DECISIONS.md
git commit -m "docs: describe the config and policy packages and record M1 decisions"
git add docs/CONFIG.md README.md docs/milestones/M1.md
git commit -m "docs: add the config reference and the M1 milestone summary"
```
