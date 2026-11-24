# M2 GitHub Adapter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Given a repository and a commit, report the CI result from GitHub Actions, and turn each failed step into a normalized, size-capped, provider-neutral failure that names the regreen check it belongs to and the files its errors mention.

**Architecture:** `internal/ci` defines the provider-neutral types and the `Provider` interface. `internal/ci/github` implements it with go-github against the Actions REST API (runs for a commit, jobs and steps, job log download). `internal/failure` is pure (no I/O): it sanitizes and caps log text, extracts file paths that exist in the repo, maps a failed step to a configured check, and assembles the `Failure` value that M3's `get_failure` will return.

**Tech Stack:** Go 1.27 standard library, `github.com/google/go-github/v93` v93.0.0 (new, pinned), `go.yaml.in/yaml/v4` (existing). Tests use `net/http/httptest` only.

**Spec:** `docs/superpowers/specs/regreen-ci-brief.md`

## Global Constraints

- Unit tests must not touch the network. Fake the GitHub API with `httptest`.
- GitHub authentication: read a token from the environment, falling back to the `gh` CLI if it is installed. Never log tokens, never write them to disk. Tokens and signed log URLs never appear in any error message or log line.
- Log text is untrusted input. Nothing in a log may ever be executed or treated as an instruction by regreen. Log-derived values are only ever compared, trimmed, and returned as data.
- Pass `context.Context` through all I/O. Wrap errors with enough context to diagnose (`fmt.Errorf("...: %w", err)`).
- Use the official go-github library at exactly `github.com/google/go-github/v93 v93.0.0`. Check signatures with `go doc` before use; do not rely on memory. In v93 the constructor is `github.NewClient(opts ...ClientOptionsFunc) (*Client, error)` with `WithAuthToken`, `WithHTTPClient`, `WithURLs(baseURL, uploadURL *string)`, `WithTimeout`, `WithUserAgent`.
- No other new dependencies. No new packages outside `internal/ci`, `internal/ci/github`, `internal/failure`.
- GitHub Actions is the only provider. Nothing GitHub-specific may appear in `internal/ci` or `internal/failure` (no GitHub log markers, no GitHub URLs, no go-github imports).
- `internal/failure` and `internal/ci` do no I/O: no `os`, `os/exec`, `net`, `net/http` imports (enforced by a test like `TestPolicyPackageIsPure` in `internal/policy/hardening_test.go`).
- Nothing writes to stdout. Library code does not log at all; it returns errors.
- Every behavior has a test. Table-driven tests with `t.Run` subtests. `make check` (vet, golangci-lint v2.15.0, `go test -race ./...`) must exit 0 at the end of every task.
- Out of scope, do not build: the 45-second wait loop, MCP tools, git operations, local state, GitLab/Jenkins, fixing tests or builds, retries, caching.
- Small focused commits, conventional prefixes (`feat(ci):`, `test(failure):`, `docs:`).

## Decisions already made by the maintainer (2026-11-24)

1. **Check mapping:** a failed step maps to a `regreen.yml` check by name/command matching only (no config change). Unmatched failures are reported as not fixable.
2. **Error files:** path-like tokens in the trimmed log of the failed step, kept only if they are tracked files (the caller supplies the set of known files).
3. **Failure timing:** status stays `pending` until every workflow run for the commit has completed.

## Assumptions (recorded in DECISIONS in Task 5)

- A1. Token lookup order: `GH_TOKEN`, then `GITHUB_TOKEN`, then `gh auth token --hostname github.com`.
- A2. Only `github.com` is supported. GitHub Enterprise hosts are rejected with a clear error.
- A3. All workflow runs whose `head_sha` equals the commit count, whatever the event (`push`, `pull_request`, ...). When the same workflow has several runs for the same event, only the newest counts.
- A4. Run conclusions `success`, `skipped`, `neutral` are passing. Everything else that is completed (`failure`, `timed_out`, `cancelled`, `startup_failure`, `action_required`, `stale`, unknown values) is failing. Fail closed.
- A5. A failed job with no failed step (cancelled, timed out, runner lost) still yields one failed entry with an empty step name, so `failure` always comes with at least one entry.
- A6. The interface is named `ci.Provider`, not `ci.CIProvider`, to avoid Go package-name stutter. Same role as the brief's `CIProvider`.
- A7. Caps: at most 8 MiB of a job log is read (the tail is kept); a trimmed excerpt is at most 200 lines, 16 KiB, 2000 bytes per line (the tail is kept); at most 100 files are extracted.
- A8. A failure ID is `gha-<runID>-<jobID>-<stepNumber>` (step number 0 when no step failed). It is stable across calls and across processes, so M3 needs no state to resolve it.

## File Structure

```
internal/ci/ci.go              Status, Repo, FailedStep, CommitStatus, Provider interface
internal/ci/remote.go          ParseRemote: git remote URL -> Repo
internal/ci/*_test.go
internal/failure/failure.go    Failure model, Build
internal/failure/trim.go       TrimLog: sanitize and cap untrusted text
internal/failure/files.go      ExtractFiles: path tokens ∩ known files
internal/failure/match.go      MatchCheck: failed step -> configured check
internal/failure/*_test.go
internal/ci/github/auth.go     Token lookup (env, then gh CLI)
internal/ci/github/provider.go New, CommitStatus
internal/ci/github/id.go       failure ID encode/parse
internal/ci/github/logs.go     FailureLog, log download, step section
internal/ci/github/*_test.go
internal/ci/github/testdata/   job log fixtures
docs/                          DECISIONS, ARCHITECTURE, README status, milestones/M2.md
```

---

### Task 1: `internal/ci` — provider-neutral types and remote parsing

**Files:**
- Create: `internal/ci/ci.go`, `internal/ci/remote.go`, `internal/ci/ci_test.go`, `internal/ci/remote_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces (exact):

```go
package ci

type Status string

const (
	StatusSuccess Status = "success"
	StatusFailure Status = "failure"
	StatusPending Status = "pending"
	StatusNoRuns  Status = "no_runs"
)

// Repo identifies a repository on a CI host.
type Repo struct{ Host, Owner, Name string }

func (r Repo) String() string // "host/owner/name"

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

type CommitStatus struct {
	Status Status
	Commit string
	Failed []FailedStep // non-empty exactly when Status == StatusFailure
}

// Provider is the brief's CIProvider.
type Provider interface {
	Name() string
	CommitStatus(ctx context.Context, repo Repo, commit string) (CommitStatus, error)
	// FailureLog returns the raw text of the failed step named by id, with
	// provider framing (timestamps, markers) removed. Untrusted data.
	FailureLog(ctx context.Context, repo Repo, id string) (string, error)
}

// ValidCommit reports whether s is a full lower-case hex object name
// (40 or 64 characters). Abbreviated or symbolic names are not accepted.
func ValidCommit(s string) bool

// ParseRemote turns a git remote URL into a Repo.
func ParseRemote(remote string) (Repo, error)
```

**Requirements:**

- `ParseRemote` accepts: `https://host/owner/name(.git)`, `http://...`, `ssh://git@host(:port)/owner/name(.git)`, `git://host/owner/name(.git)`, and scp-like `git@host:owner/name(.git)`. One optional trailing `/` and one optional `.git` suffix are removed. Host is lower-cased; owner and name keep their case. Userinfo and port are dropped and never appear in the result or in error messages (a remote can carry `https://user:token@host/...`; the error must not echo the input string, only describe the problem).
- Rejects with an error: empty input, surrounding whitespace or any control character, missing owner or name, more than two path segments, owner or name not matching `^[A-Za-z0-9._-]+$`, name `.` or `..`, unknown scheme, local paths (`/srv/repo.git`, `../repo`, `file://`).
- `ValidCommit`: only `[0-9a-f]`, length exactly 40 or 64.
- A purity test asserts the package's non-test files import only the standard-library packages they need and none of `os`, `os/exec`, `net`, `net/http` (`net/url` is allowed). Model it on `TestPolicyPackageIsPure`.

**Tests (table-driven, at minimum):**

| input | result |
|---|---|
| `https://github.com/Tawfiq-alqaidy/regreen-ci.git` | `{github.com Tawfiq-alqaidy regreen-ci}` |
| `https://github.com/o/r` and `https://github.com/o/r/` | `{github.com o r}` |
| `git@github.com:o/r.git` | `{github.com o r}` |
| `ssh://git@GitHub.com:22/o/r.git` | `{github.com o r}` |
| `https://user:secret@github.com/o/r.git` | `{github.com o r}` |
| `https://user:secret@github.com/o` | error; message does not contain `secret` |
| `https://github.com/o/r/extra`, `https://github.com//r`, `git@github.com:r.git` | error |
| `/srv/git/r.git`, `file:///srv/r.git`, `ftp://h/o/r`, `""`, `" https://github.com/o/r"`, `"https://github.com/o/r\n"` | error |
| `https://github.com/o/..`, `https://github.com/o/a b` | error |

`ValidCommit`: 40 hex true; 64 hex true; 39, 41, upper-case, `HEAD`, empty, 40 chars with `g` false.

- [ ] **Step 1:** Write `remote_test.go` and `ci_test.go` (ValidCommit table, `Repo.String`, purity test) with the tables above.
- [ ] **Step 2:** Run `go test ./internal/ci/` and confirm it fails to compile (types missing).
- [ ] **Step 3:** Write `ci.go` and `remote.go` to the interface block above. Every exported identifier gets a doc comment.
- [ ] **Step 4:** Run `go test -race ./internal/ci/` until it passes, then `make check`.
- [ ] **Step 5:** Commit: `feat(ci): add provider-neutral CI types and remote URL parsing`.

---

### Task 2: `internal/failure` — the normalized failure model

**Files:**
- Create: `internal/failure/failure.go`, `trim.go`, `files.go`, `match.go`, and `failure_test.go`, `trim_test.go`, `files_test.go`, `match_test.go`, `fuzz_test.go`

**Interfaces:**
- Consumes: `ci.FailedStep` (Task 1: fields `ID, Workflow, Job, Step string; StepNumber int; URL string`), `config.Check` (existing: `struct{ Name, Run, Fix string }` in `internal/config/repo.go`).
- Produces (exact):

```go
package failure

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

type Limits struct{ MaxLines, MaxBytes, MaxLineBytes, MaxFiles int }

func DefaultLimits() Limits // 200, 16384, 2000, 100

// TrimLog sanitizes and caps untrusted text, keeping the tail.
func TrimLog(raw string, lim Limits) (text string, truncated bool)

// ExtractFiles returns the known files mentioned in text.
func ExtractFiles(text string, known map[string]struct{}, max int) []string

// MatchCheck finds the configured check a failed step belongs to.
func MatchCheck(job, step string, checks []config.Check) (config.Check, bool)

// Build assembles a Failure. log is the provider's raw step log.
func Build(step ci.FailedStep, commit, log string, checks []config.Check, known map[string]struct{}, lim Limits) Failure
```

**Requirements — `TrimLog`:**

1. Replace invalid UTF-8 with U+FFFD. Normalize `\r\n` and lone `\r` to `\n`.
2. Remove ANSI escape sequences: CSI (`ESC [` … final byte `@`–`~`) and OSC (`ESC ]` … terminated by BEL or `ESC \`); a lone `ESC` is dropped.
3. Remove every remaining control character (Unicode category Cc) except `\n` and `\t`, and remove the bidirectional override/isolate characters U+202A–U+202E and U+2066–U+2069 (they can visually reorder text shown to a human or agent).
4. Trim trailing spaces and tabs on each line. Collapse runs of 3 or more blank lines to 2. Drop leading and trailing blank lines.
5. A line longer than `MaxLineBytes` is cut at a rune boundary and ` …[line cut]` is appended; this sets `truncated`.
6. If more than `MaxLines` lines or more than `MaxBytes` bytes remain, keep the longest tail that fits both, and put `[… N earlier lines trimmed …]` as the first line (N is the number of dropped lines; the marker line counts toward neither cap). This sets `truncated`.
7. Non-positive limit fields are replaced by the `DefaultLimits` value for that field. The result never ends with `\n`. Empty or all-blank input returns `("", false)`.
8. Idempotent for untruncated output: `TrimLog(TrimLog(x).text)` returns the same text.

**Requirements — `ExtractFiles`:**

1. Split text into tokens on any whitespace and on the characters `"'` `` ` `` `()[]{}<>,;=|`.
2. For each token: skip if longer than 4096 bytes; replace `\` with `/`; strip a trailing position suffix, repeatedly: `:<digits>` (covers `:12` and `:12:5`), a trailing `:`, a trailing `.` or `,`; also strip a `(<digits>)` or `(<digits>,<digits>)` suffix when it was part of the token before splitting — since `(` is a delimiter, `file.ts(12,5)` already splits into `file.ts`, which is enough; strip a leading `./` (repeatedly).
3. Candidate lookup: if the cleaned token is in `known`, take it. Otherwise, only if the token is absolute (starts with `/` or matches `^[A-Za-z]:/`), try each suffix that begins right after a `/`, longest first, and take the first that is in `known` (this maps `/home/runner/work/app/app/src/a.ts` to `src/a.ts`). Relative tokens must match exactly; `x/src/a.ts` does not match known `src/a.ts`.
4. A token containing a `..` segment is never matched.
5. Result is sorted, unique, at most `max` entries (non-positive `max` means `DefaultLimits().MaxFiles`); when more match, the first `max` in sorted order are kept.
6. Nil or empty `known` returns nil. The function never touches the filesystem.

**Requirements — `MatchCheck`:**

Normalize a string with `norm`: trim, collapse every whitespace run to one space. `stepCmd` = `norm(step)` with one leading `Run ` removed if present (GitHub's default step name is `Run <first line of the script>`).

Priority, first level that yields exactly one check wins; a level that yields two or more different checks returns `(config.Check{}, false)` immediately (ambiguous, fail closed):

1. `stepCmd` equals `norm(check.Run)` exactly, or `norm(step)` equals `check.Name` case-insensitively.
2. `stepCmd` contains `norm(check.Run)` at token boundaries (the match is preceded by start-of-string or a space, and followed by end-of-string or a space). When several checks match here, the one with the longest `norm(check.Run)` wins; a tie between different checks is ambiguous.
3. `norm(job)` equals `check.Name` case-insensitively.

Empty `step` skips levels 1 and 2. A check whose `norm(Run)` is empty never matches by command. No match returns `(config.Check{}, false)`.

**Requirements — `Build`:** copies ID, Workflow, Job, Step, URL from `step` and `commit` into `Commit`; `Errors, Truncated = TrimLog(log, lim)`; `Files = ExtractFiles(Errors, known, lim.MaxFiles)`; `MatchCheck(step.Job, step.Step, checks)` sets `Check`, `Command`, `Fixable`. Files are extracted from the trimmed text, so a path that only appears in a dropped part of the log does not widen the edit scope.

**Tests (at minimum):**

`TrimLog`: plain text unchanged; CRLF normalized; `"\x1b[31merror\x1b[0m"` → `error`; OSC hyperlink removed; NUL, BEL, `\x7f` removed; U+202E removed; tab kept; invalid UTF-8 byte → U+FFFD; 4 blank lines → 2; over-long line cut at a rune boundary (use a multi-byte rune straddling the limit) with the marker and `truncated`; 500 short lines with `MaxLines: 10` → marker line `[… 490 earlier lines trimmed …]` plus the last 10; byte cap smaller than line cap; zero `Limits{}` behaves like defaults; empty and all-blank input; idempotence over every table input.

`ExtractFiles` with `known = {src/a.ts, src/b.ts, main.go, internal/x/y.go, "docs/my file.md"}`:

| text | files |
|---|---|
| `src/a.ts:12:5 - error TS2322` | `src/a.ts` |
| `./src/a.ts(12,5): error` | `src/a.ts` |
| `/home/runner/work/app/app/src/b.ts:3` | `src/b.ts` |
| `D:\a\app\app\src\b.ts:3:1` | `src/b.ts` |
| `main.go:10:2: unused variable` and `internal/x/y.go:1: x` | `internal/x/y.go`, `main.go` |
| `see "src/a.ts", and (src/b.ts).` | `src/a.ts`, `src/b.ts` |
| `x/src/a.ts:1` | none |
| `../src/a.ts`, `/etc/../src/a.ts` | none |
| `src/c.ts:1` (not known) | none |
| `docs/my file.md` | none (whitespace splits; documented limit) |
| same file mentioned 3 times | once |
| `max = 1` with two matches | first in sorted order |
| nil `known` | nil |

`MatchCheck` with checks `lint{Run: "npm run lint"}`, `types{Run: "npx tsc --noEmit"}`, `fmt{Run: "npm run"}`:

| job, step | result |
|---|---|
| `build`, `Run npm run lint` | lint (level 1 exact) |
| `build`, `Run npm   run lint ` | lint |
| `build`, `Run npm run lint -- --max-warnings 0` | lint (level 2, longest beats `npm run`) |
| `build`, `Run npm run` | fmt |
| `build`, `Lint` | lint (name, case-insensitive) |
| `types`, `Check code` | types (level 3) |
| `build`, `Run xnpm run lint` | no match (token boundary) |
| `build`, `Run go test ./...` | no match |
| `lint`, `` (no step) | lint (level 3) |
| two checks with the same `Run`, step `Run <that>` | no match (ambiguous) |
| `checks` nil | no match |

`Build`: one case mapped (Fixable true, Check/Command set, Files from the log, IDs copied), one unmapped (Fixable false, Check and Command empty, Errors still present).

`fuzz_test.go`: `FuzzTrimLog` (output is valid UTF-8, contains no `\x1b`, no Cc except `\n`/`\t`, at most `MaxLines`+1 lines, never panics) and `FuzzExtractFiles` (every result is in `known`, sorted, unique, never panics). Seed each with the table inputs. Run each for 20 seconds once locally (`go test -run '^$' -fuzz FuzzTrimLog -fuzztime 20s ./internal/failure/`) and report the result.

A purity test as in Task 1 (`internal/failure` may import `internal/ci`, `internal/config`, and standard-library packages other than `os`, `os/exec`, `net`, `net/http`).

- [ ] **Step 1:** Write the four test files with the tables above. Run `go test ./internal/failure/`; confirm compile failure.
- [ ] **Step 2:** Implement `trim.go`; run its tests until green. Commit: `feat(failure): sanitize and cap untrusted log text`.
- [ ] **Step 3:** Implement `files.go`; tests green. Commit: `feat(failure): extract known files from log text`.
- [ ] **Step 4:** Implement `match.go`; tests green. Commit: `feat(failure): map a failed step to a configured check`.
- [ ] **Step 5:** Implement `failure.go` (`Failure`, `Limits`, `DefaultLimits`, `Build`) and the purity test; tests green.
- [ ] **Step 6:** Add `fuzz_test.go`, run both fuzzers 20s each, fix anything found (add the failing input as a table row).
- [ ] **Step 7:** `make check`, then commit: `feat(failure): add the normalized failure model`.

---

### Task 3: `internal/ci/github` — authentication, client, commit status

**Files:**
- Create: `internal/ci/github/auth.go`, `provider.go`, `id.go`, `auth_test.go`, `provider_test.go`, `id_test.go`, `fake_test.go`
- Modify: `go.mod`, `go.sum` (`go get github.com/google/go-github/v93@v93.0.0`, then `go mod tidy`)

**Interfaces:**
- Consumes: `ci.Repo`, `ci.Status*`, `ci.FailedStep`, `ci.CommitStatus`, `ci.Provider`, `ci.ValidCommit` (Task 1).
- Produces (exact):

```go
package github

const ProviderName = "github-actions"

// ErrNoToken means no token was found in the environment or via gh.
var ErrNoToken = errors.New("no GitHub token found: set GH_TOKEN or GITHUB_TOKEN, or run `gh auth login`")

// Token returns a GitHub token from GH_TOKEN, then GITHUB_TOKEN, then
// `gh auth token --hostname github.com`. It never logs the token.
func Token(ctx context.Context) (string, error)

type Option func(*options)

func WithBaseURL(u string) Option         // API base, for tests; must end with "/"
func WithHTTPClient(c *http.Client) Option // transport for API calls, for tests

// New returns a provider that authenticates with token.
func New(token string, opts ...Option) (*Provider, error)

type Provider struct{ /* unexported */ }

func (p *Provider) Name() string // ProviderName
func (p *Provider) CommitStatus(ctx context.Context, repo ci.Repo, commit string) (ci.CommitStatus, error)

// unexported, used by Task 4:
func encodeID(runID, jobID int64, step int) string            // "gha-<run>-<job>-<step>"
func parseID(id string) (runID, jobID int64, step int, err error)
```

**Requirements — `Token`:**

- Implemented over an unexported `token(ctx, getenv func(string) string, gh func(context.Context) (string, error))` so tests need no real environment. `Token` passes `os.Getenv` and a function that runs `gh`.
- A value is trimmed of surrounding whitespace; an empty value counts as unset and lookup continues. A non-empty value containing whitespace or control characters is an error (`invalid token in GH_TOKEN` — name the source, never the value).
- The `gh` runner: `exec.LookPath("gh")`; not found → `ErrNoToken`. Runs `gh auth token --hostname github.com` with `exec.CommandContext`, stdout captured, stderr discarded, with a 10 second timeout derived from ctx. Non-zero exit → `ErrNoToken` wrapped with `gh auth token failed` (do not include gh's output).
- No function in this package ever formats a token into an error, a log line, or a `String()` method. `Provider` does not store the token in an exported or printable field: implement `func (p *Provider) String() string` returning `"github-actions provider"` so `%v`/`%+v` cannot dump it.

**Requirements — `New`:**

- Empty token → error. Builds the client with `github.NewClient(github.WithAuthToken(token), github.WithUserAgent("regreen-ci"), github.WithTimeout(30*time.Second), ...)`; `WithBaseURL` maps to `github.WithURLs(&u, nil)`, `WithHTTPClient` to `github.WithHTTPClient`. Verify option order and semantics with `go doc` (for example whether `WithHTTPClient` and `WithTimeout` conflict) and note what you found in the report.
- The provider remembers whether the base URL was overridden (Task 4 needs it).

**Requirements — `CommitStatus`:**

1. Validate: `repo.Host` must be `github.com` (error: `host %q is not supported: only github.com`); owner and name non-empty; `ci.ValidCommit(commit)` (error names the problem: `commit must be a full 40 or 64 character hex object name`).
2. List runs: `Actions.ListRepositoryWorkflowRuns` with `HeadSHA: commit`, `PerPage: 100`, following `resp.NextPage` until 0, at most 10 pages (more → error `too many workflow runs`). Ignore any returned run whose `GetHeadSHA()` differs from `commit` (defensive; the filter is server-side).
3. No runs → `{Status: no_runs, Commit: commit}`.
4. Group runs by `(GetWorkflowID(), GetEvent())`; in each group keep the run with the highest `GetRunNumber()`, ties broken by highest `GetRunAttempt()`.
5. If any kept run has `GetStatus() != "completed"` → `{Status: pending, Commit: commit}` with no `Failed` entries and no further API calls.
6. A kept run passes when its conclusion is `success`, `skipped`, or `neutral`. All pass → `{Status: success}`.
7. For each failing run, in ascending run ID order: `Actions.ListWorkflowJobs` with `Filter: "latest"`, `PerPage: 100`, paginated (cap 10 pages). A job fails when its status is `completed` and its conclusion is not `success`, `skipped`, or `neutral`. For each failing job in the order returned:
   - one `ci.FailedStep` per step with conclusion `failure`: `ID: encodeID(runID, jobID, stepNumber)`, `Workflow: run.GetName()`, `Job: job.GetName()`, `Step: step.GetName()`, `StepNumber`, `URL: job.GetHTMLURL()`;
   - if the job has no step with conclusion `failure`: one entry with `Step: ""`, `StepNumber: 0`.
   - A failing run with no failing job: one entry with `ID: encodeID(runID, 0, 0)`, `Job: ""`, `Step: ""`, `URL: run.GetHTMLURL()`.
8. Result: `{Status: failure, Commit: commit, Failed: ...}`.
9. Errors from the API are wrapped with the operation and `repo.String()`: `list workflow runs for github.com/o/r: %w`. Use `errors.As` in tests to show `*github.RateLimitError` and `*github.ErrorResponse` (404) stay reachable through the wrap. `ctx` is passed to every call; a cancelled context returns an error that satisfies `errors.Is(err, context.Canceled)`.

**Requirements — IDs:** `parseID` accepts only `^gha-(\d{1,19})-(\d{1,19})-(\d{1,9})$` with values that fit `int64`/`int`, run ID > 0; anything else is an error that quotes the input with `%q`. `encodeID`/`parseID` round-trip.

**Tests (`httptest.Server`, no network):** `fake_test.go` holds a small fake GitHub API: a `http.ServeMux` serving `/repos/{owner}/{repo}/actions/runs` and `/repos/{owner}/{repo}/actions/runs/{id}/jobs` from in-memory fixtures, recording each request (path, query, `Authorization` header). Cases:

- token: env `GH_TOKEN` wins over `GITHUB_TOKEN`; `GITHUB_TOKEN` used when `GH_TOKEN` empty or whitespace; gh fallback used when both unset; gh error → `errors.Is(err, ErrNoToken)`; token with an inner space → error that names the source and does not contain the value; one test puts a fake `gh` shell script in `t.TempDir()`, prepends it to `PATH` with `t.Setenv`, and checks the real runner returns its output trimmed (skip on Windows); one test with an empty `PATH` → `ErrNoToken`.
- requests carry `Authorization: Bearer <token>` and the `head_sha` and `per_page=100` query parameters.
- no runs → `no_runs`; one in-progress run → `pending` and the jobs endpoint is never called; all success → `success`; `skipped` and `neutral` count as passing.
- one failed run, one failed job, one failed step → one entry with every field checked, including the ID.
- two failed steps in one job; two failed jobs; a failed job without a failed step (cancelled) → entry with empty step; a `startup_failure` run with zero jobs → run-level entry.
- same workflow and event twice (run numbers 7 and 8): 7 failed, 8 success → `success`. Same workflow, events `push` and `pull_request`: push success, pull_request failure → `failure`.
- one run completed with failure and another still queued → `pending`.
- pagination: 150 runs across two pages (the fake sets a `Link` header with `rel="next"`) are all seen.
- a run with a different `head_sha` in the response is ignored.
- errors: 404 → `errors.As` to `*github.ErrorResponse`, message contains `github.com/o/r`; 403 with `X-RateLimit-Remaining: 0` → `*github.RateLimitError`; cancelled context; host `gitlab.com` → error; short commit → error; `New("")` → error.
- no error message in any test contains the token (assert with a helper used in every error case).
- `fmt.Sprintf("%+v", provider)` does not contain the token.
- compile-time check: `var _ ci.Provider = (*Provider)(nil)` goes in Task 4 once `FailureLog` exists; in this task assert `Name()` only.

- [ ] **Step 1:** `go get github.com/google/go-github/v93@v93.0.0 && go mod tidy`; read `go doc` for every go-github symbol named above.
- [ ] **Step 2:** Write `id_test.go` and `auth_test.go`; confirm they fail; implement `id.go` and `auth.go`; green. Commit: `feat(github): look up the GitHub token from env or gh`.
- [ ] **Step 3:** Write `fake_test.go` and `provider_test.go`; confirm they fail; implement `provider.go`; green.
- [ ] **Step 4:** `make check`. Commit: `feat(github): report CI status for a commit from GitHub Actions`.

---

### Task 4: `internal/ci/github` — failed step logs

**Files:**
- Create: `internal/ci/github/logs.go`, `logs_test.go`, `testdata/job_lint_failed.log`, `testdata/job_no_timestamps.log`
- Modify: `internal/ci/github/fake_test.go` (add job and log endpoints), `internal/ci/github/provider_test.go` (compile-time interface check)

**Interfaces:**
- Consumes: `Provider`, `parseID`, the fake API (Task 3); `ci.Provider` (Task 1).
- Produces:

```go
// FailureLog implements ci.Provider.
func (p *Provider) FailureLog(ctx context.Context, repo ci.Repo, id string) (string, error)

// unexported, pure:
func stepSection(raw string, start, end time.Time) string
func stripFraming(section string) string
```

**Background — the real format** (checked against this repository's own CI on 2026-11-24): a job log is one plain-text file. It starts with a UTF-8 BOM. Every line is `<timestamp> <text>` where the timestamp is RFC 3339 with 7 fractional digits and `Z`, for example `2026-11-24T10:25:38.1483444Z ##[group]Run golangci/golangci-lint-action@ba0d…`. Steps are not separated by any reliable marker; a `run:` step begins with `##[group]Run <first line of script>`, and a failing one ends with `##[error]Process completed with exit code 1.`. The jobs API gives each step's `started_at` and `completed_at` at one-second resolution. Marker prefixes seen: `##[group]`, `##[endgroup]`, `##[error]`, `##[warning]`, `##[notice]`, `##[debug]`, `##[command]`, `[command]`.

**Requirements — `FailureLog`:**

1. Validate repo as in `CommitStatus`. `parseID(id)`; job ID 0 (a run-level failure) → return `""`, nil: there is no job log.
2. `Actions.GetWorkflowJobByID(ctx, owner, name, jobID)`. If the job's `GetRunID()` differs from the ID's run ID → error (`failure id does not match job`). Find the step with `GetNumber() == step` (step 0: no step, use the whole log).
3. `Actions.GetWorkflowJobLogs(ctx, owner, name, jobID, 2)` returns the signed download URL. Never put this URL in an error or log: on failure report only the operation and, if any, the HTTP status.
4. Download with a separate plain `http.Client` (30s timeout, redirects limited to 2) that sends **no** `Authorization` header: the URL is on a different host and is already signed; the token must not travel there. The URL scheme must be `https`, except when the provider was built with `WithBaseURL` using an `http://` URL (tests). Non-200 → error with the status code.
5. Read at most 8 MiB, keeping the **tail**: when the body is larger, keep the last 8 MiB and drop the first partial line. Do not buffer more than about twice the cap.
6. `stepSection(raw, step.StartedAt, step.CompletedAt)` then `stripFraming`. Return the result.

**Requirements — `stepSection`:**

- Strip one leading UTF-8 BOM. Split into lines. A line "has a timestamp" when its first space-delimited field parses as RFC 3339 (`time.Parse(time.RFC3339Nano, …)`).
- Keep lines whose timestamp `t` satisfies `start <= t < end + 1s` (API times are truncated to the second, so the step's last lines can carry fractions past `end`). A line without a timestamp belongs with the previous line (kept if that one was kept).
- If `start` or `end` is zero, or `end` is before `start`, or no line would be kept, return the whole input (BOM stripped): a longer excerpt is better than an empty one, and the caller caps it anyway.

**Requirements — `stripFraming`:**

- Remove the leading timestamp and one following space from each line that has one.
- Drop lines that are exactly `##[endgroup]`. Remove these prefixes, keeping the rest of the line: `##[group]`, `##[command]`, `[command]`, `##[debug]`. Replace `##[error]` with `error: `, `##[warning]` with `warning: `, `##[notice]` with `notice: `.
- No other transformation: sanitizing and capping is `failure.TrimLog`'s job.

**Fixtures:** `testdata/job_lint_failed.log` is a hand-written log in the real format (BOM, timestamps) for a job with steps: 1 `Set up job` (10:00:00–10:00:02), 2 `Run actions/checkout@…` (10:00:02–10:00:04), 3 `Run npm run lint` (10:00:04–10:00:09, fails), 4 `Complete job`. Step 3's lines include `##[group]Run npm run lint`, shell/env lines, `##[endgroup]`, an eslint-style block with an ANSI color sequence and paths `/home/runner/work/app/app/src/a.ts` with `12:5  error  'x' is assigned a value but never used  no-unused-vars`, a line with fraction `10:00:09.9000000Z`, and `##[error]Process completed with exit code 1.`. Include one line in step 2 that mentions `src/secret.ts` so the test can prove other steps' text is excluded. `testdata/job_no_timestamps.log` is the same content with no timestamps (exercises the fallback). Keep both under 60 lines. Do not copy real logs.

**Tests:**

- `stepSection`: selects exactly step 3's lines from the fixture, including the `.9000000Z` line at the end boundary and excluding step 4's first line at `10:00:10`; zero times → whole log; no timestamps → whole log; window matching nothing → whole log; BOM removed; a multi-line message whose continuation lines lack timestamps stays together.
- `stripFraming`: timestamps removed; `##[endgroup]` lines dropped; each prefix handled; a line containing `##[error]` in the middle is untouched.
- `FailureLog` through the fake API (which serves `GET /repos/o/r/actions/jobs/{id}` and answers `GET /repos/o/r/actions/jobs/{id}/logs` with a 302 to `/download/{id}` on the same test server): returns step 3's text without framing and without `src/secret.ts`; the download request carries **no** `Authorization` header while the API requests do; step 0 returns the whole log without framing; job ID 0 returns `""` with no API call; run ID mismatch → error; unknown step number → whole log; malformed ID → error; download 404 and 500 → error containing the status code and **not** containing the download URL or its query string (the fake adds `?sig=SECRETSIG` to the redirect; assert `SECRETSIG` appears in no error); a 9 MiB body → result is at most 8 MiB and ends with the body's last line; a redirect to a non-https host when the base URL is https → error (unit-test the scheme check as a small function); cancelled context → `errors.Is(err, context.Canceled)`.
- End to end with `failure.Build` (import `internal/failure` and `internal/config` in the test only): fixture log → `Failure` with `Check: "lint"`, `Fixable: true`, `Files: ["src/a.ts"]` given `known = {src/a.ts, src/secret.ts}`, and `Errors` contains no `\x1b` and no timestamp.
- `var _ ci.Provider = (*Provider)(nil)`.
- `live_test.go` with build-independent opt-in: `TestLive` is skipped unless `REGREEN_LIVE_REPO` (`owner/name`) and `REGREEN_LIVE_COMMIT` are set; it calls `Token`, `New`, `CommitStatus`, and for each failed step `FailureLog`, logging the status and the first 20 lines with `t.Log`. It never runs in CI and never logs the token.

- [ ] **Step 1:** Write the two fixtures and `logs_test.go` unit tests for `stepSection` and `stripFraming`; confirm they fail; implement both; green. Commit: `feat(github): cut a failed step's lines out of a job log`.
- [ ] **Step 2:** Extend the fake API; write the `FailureLog` tests; confirm they fail; implement `FailureLog` and the bounded tail reader; green.
- [ ] **Step 3:** Add the end-to-end `Build` test, the interface assertion, and `live_test.go`.
- [ ] **Step 4:** `make check`. Commit: `feat(github): fetch the log of a failed step`.

---

### Task 5: Documentation and milestone summary

**Files:**
- Modify: `docs/DECISIONS.md` (append D31 onward), `docs/ARCHITECTURE.md`, `README.md` (status section)
- Create: `docs/milestones/M2.md`

**Requirements:**

- Read the code as it actually exists first (`internal/ci`, `internal/ci/github`, `internal/failure`) and the SDD ledger's rulings; document what was built, not what this plan said. Where they differ, the code wins and the difference is mentioned.
- `DECISIONS.md`, same format as D1–D30 (decision, alternatives, why, cost if wrong), one entry each for: go-github v93.0.0 as the GitHub client (and why not raw `net/http`; note it brings `go-querystring`); the `ci.Provider` interface shape and name (A6); stateless failure IDs (A8); which runs count and when status is final (maintainer decision 3, A3); conclusion mapping, fail closed (A4, A5); step log by timestamp window, with fallback (and why not the run-logs zip archive); log download without the token, tail cap (A7); token lookup order and github.com only (A1, A2); check mapping by name/command (maintainer decision 1) with its ambiguity rule; error files = log tokens ∩ known files (maintainer decision 2), how this resolves D30 and what M3 must still do (supply known files from the original failing commit or the base, not from the agent's later push); `TrimLog` sanitizing rules (ANSI, control and bidi characters).
- `ARCHITECTURE.md`: update "What exists today" to M0–M2 with a section for the CI adapter and failure model (data flow: `ParseRemote` → `CommitStatus` → `FailureLog` → `failure.Build`), update "Layout" and "Trust model" (log text is untrusted; what M2 does about it; what remains for M3: delimiting the excerpt as data in the tool output). Add a short "Notes for M3" list: the 45-second wait loop polls `CommitStatus`; known files come from git; `Failure.Fixable == false` must be reported, not fixed; printers must quote log-derived strings.
- `docs/milestones/M2.md`, plain language for a maintainer learning Go, same structure as `docs/milestones/M1.md`: what was built; how the pieces connect; how to try it by hand (the `TestLive` command with the two environment variables, and `go test -run TestFailureLog -v ./internal/ci/github/`); Go idioms that are not obvious (interfaces satisfied implicitly and the `var _ ci.Provider = (*Provider)(nil)` assertion; functional options; `httptest.Server`; `errors.Is`/`errors.As` through `%w`; `io.LimitReader` and bounded reads; pointer fields and `GetX()` accessors in go-github; fuzz tests); known limitations (file names with spaces are not extracted; steps are cut by one-second timestamps so adjacent fast steps can bleed into each other; composite actions and reusable workflows report as their calling step; GitHub Enterprise unsupported; no retries on rate limits); honest limit: a mapped, trimmed failure is still untrusted text.
- `README.md`: status line says M0–M2 are done and what M2 added, in two or three sentences. No feature is described as working that is not.
- No emoji. Wrap nothing artificially. Every command shown has been run and its output checked.

- [ ] **Step 1:** Read the code and the ledger; list any difference from this plan.
- [ ] **Step 2:** Write the DECISIONS entries. Commit: `docs: record M2 decisions`.
- [ ] **Step 3:** Update ARCHITECTURE and README. Commit: `docs: describe the CI adapter and failure model`.
- [ ] **Step 4:** Write `docs/milestones/M2.md`, running each command it shows. `make check`. Commit: `docs: add the M2 milestone summary`.
