# Architecture

regreen is one binary with several subcommands over one shared core. This document describes the intended layout and marks what exists today. It is updated at the end of every milestone.

## What exists today (M0 and M1)

Every statement in this section must be checked against the code at the end of each milestone.

### The command line (M0)

```
cmd/regreen/
  main.go      main(): passes os.Args[1:], os.Stdout, os.Stderr to run
  app.go       run(): command dispatch, usage text, exit codes
  version.go   resolveVersion(): ldflags value, then Go build info, then "dev"
```

A plain `go build` inside a git checkout reports a pseudo-version such as `v0.0.0-<date>-<commit>+dirty`, because the Go toolchain stamps the binary with the commit it was built from. `dev` is reported when there is no usable build information (none, an empty version, or `(devel)`).

`main()` contains no logic. `run(args, stdout, stderr)` returns the exit code, so tests call it in-process with in-memory buffers instead of starting a subprocess.

Output discipline: requested output (help, version) goes to stdout, and every diagnostic goes to stderr. This matters because `regreen mcp` will speak the MCP protocol over stdio, where stdout is reserved for the protocol and one stray print breaks the server.

Exit codes: `0` success, `1` error, `2` usage error (no command, or an unknown one).

`-h` and `--help` are aliases of `help`, and `--version` is an alias of `version`. The planned commands (`mcp`, `verify`, `init`, `watch`) are listed in help, marked "(planned)", but exit 1 with "not implemented yet (planned for milestone Mx)" on stderr.

### The policy engine and config loading (M1)

Nothing calls these two packages from the command line yet. They are libraries with tests; `regreen verify`, `regreen init` and the MCP server will use them in later milestones.

```
internal/policy/        pure policy engine; standard library only, no I/O
  diff.go       Diff, FileChange, Line, ChangeKind: the model of a patch
  glob.go       the gitignore-style pattern matcher: ValidatePattern, MatchPath
  paths.go      checkRepoPath: the one path check the parser and Evaluate share
  protected.go  the always-protected path list; IsProtected, AlwaysProtected
  parse.go      ParseUnifiedDiff: patch text from git -> Diff, failing closed
  policy.go     Rules, Input, Verdict, Reason, the Rule* names; Evaluate

internal/config/        reads files and turns them into policy.Rules
  repo.go       ParseRepo, LoadRepo: regreen.yml -> Repo (checks + policy.Rules)
  user.go       LoadUser, UserConfigPath: the per-user config file
```

`internal/policy` may import only the standard library. A test (`TestPolicyPackageIsPure`) reads the imports of every non-test file and fails if one of them is not on an allowlist of the exact standard-library packages the engine uses today (`errors`, `fmt`, `path`, `slices`, `strconv`, `strings`, `unicode/utf8`). A new import, even from the standard library, has to be added to that list on purpose. `internal/config` imports `internal/policy`, never the other way round.

`MatchPath` compares pattern segments with path segments by dynamic programming: one row of a table per pattern segment, one column per path segment, so a match costs at most (pattern segments) x (path segments) segment comparisons however many `**` the pattern has. Runs of `**` are collapsed first. The last row answers "does the pattern match the whole path or any leading directory of it" for every prefix length at once.

Paths are checked by one function, `checkRepoPath`, called by both the parser and `Evaluate`. Besides absolute paths, `..`, backslashes and paths that are not already clean, it rejects control characters (bytes below 0x20 and DEL), invalid UTF-8, paths over 4096 bytes and paths with more than 255 segments. Every path and pattern a reason message names is printed quoted (Go's `%q`), so a file name cannot put a raw newline or terminal escape into a log.

Data flow:

```
regreen.yml bytes --config.ParseRepo--> config.Repo{Checks, Policy policy.Rules}
patch text        --policy.ParseUnifiedDiff--> policy.Diff
policy.Input{Diff, Rules, ErrorFiles, AttemptsUsed} --policy.Evaluate--> policy.Verdict
```

`ParseRepo` takes bytes, not a path, so a caller that must not trust the working tree can pass bytes it read from the base branch (`regreen verify`, M4). `LoadRepo` and `LoadUser` read a file and call the same parsing code.

`Evaluate` takes four things and nothing else: the diff, the rules, the files named in the failure's errors, and the number of attempts already used. There is no input for anything the agent says about its own work, and no state file. It reports every violation it finds, as a list of `Reason` values, each naming a rule from a fixed set. The order is:

1. Rules that are themselves invalid (`invalid_rules`): a limit out of range, an unknown `edit_scope`, an invalid `protected_files` pattern, an empty forbidden pattern or one containing a newline, a negative attempt count. If there are any, `Evaluate` returns only these reasons and stops, before it looks at any path.
2. `max_attempts`: attempts used is not below the limit.
3. `empty_diff`: the diff changes no files.
4. For each file, in the order the patch lists them:
   1. `invalid_path` (the path fails `checkRepoPath`, above), or `invalid_diff` for a malformed `Diff` value (a negative deletion count, a duplicate path, an unknown change kind, an unknown `Special` value, a binary change with line counts, an added line numbered below 1). If either applies, the remaining per-file checks are skipped for that file.
   2. `protected_file`: the always-protected list, then `protected_files` from the config.
   3. `edit_scope`: with `files_in_errors`, the file must be named in the errors.
   4. `binary_file`.
   5. `special_file`: symlink, submodule, mode change, or a new file with mode 100755 (executable).
   6. `forbidden_pattern`: one reason per added line and pattern that matches. At most 20 of these are returned; one more reason says how many were left out.
5. `max_deleted_lines`, then `max_changed_lines` (added plus deleted), over the whole diff.

The diff is allowed exactly when the list of reasons is empty. `AttemptsRemaining` is `max(0, max_attempts - attempts used)` and is reported whether or not the diff is allowed. It is computed before the attempt being judged is counted: an allowed submit with 1 of 2 attempts used reports 1, and once that fix is pushed (and so counted), 0 remain.

The diff parser expects the output of `git -c core.quotepath=off diff --no-color --no-ext-diff --no-textconv --no-renames --ignore-submodules=none <base> <head>` and rejects anything it does not recognize, because the engine judges only what the parser reports. Inside a hunk, the only backslash line it accepts is `\ No newline at end of file`; on mode and `index` lines it accepts only the modes 100644, 100755, 120000 and 160000. The code that runs git (`internal/vcs`, M3) must follow the [git invocation checklist](#git-invocation-checklist-for-m3m4-planned) below. Most settings it pins only change the output format, which the parser rejects (failing closed, but making `regreen` unusable on a machine with such a setting); a few make git leave changes out, which the parser cannot detect.

## Layout

Packages marked "built" exist; the rest are planned.

| Package | Responsibility | Milestone |
|---|---|---|
| `cmd/regreen/` | Entry point and subcommand wiring | M0 (built) |
| `internal/config/` | Repo config (`regreen.yml`) and user config: loading and validation | M1 (built) |
| `internal/policy/` | The policy engine. Pure functions, no I/O: takes a diff and a rule set, returns a verdict with reasons. It also holds the unified-diff parser (decision D17) | M1 (built) |
| `internal/failure/` | The normalized failure model | M2 |
| `internal/ci/` | The `CIProvider` interface | M2 |
| `internal/ci/github/` | The GitHub Actions adapter | M2 |
| `internal/vcs/` | Git operations: diff, commit, push, trailers | M3 |
| `internal/state/` | Local state (claimed commits), stored outside the repo | M3 |
| `internal/mcpserver/` | The MCP tools, a thin layer over the core | M3 |
| `internal/watch/` | Background watcher | M6 |
| `presets/` | YAML presets, embedded with `go:embed` | M5 |

## Planned flow

1. The developer or their agent pushes a commit.
2. The agent calls `wait_for_ci`. regreen asks the CI provider for the result of that commit.
3. On failure, the agent calls `get_failure` and receives a normalized, trimmed failure. Log text is untrusted data and is never executed.
4. The agent calls `apply_autofix`. If the project's own fix command resolves the failure, the deterministic change is committed on its own.
5. If errors remain, the agent edits files and calls `submit_fix`. regreen reads the diff, runs the policy engine, runs every configured check, and only then commits and pushes.
6. In CI, `regreen verify` applies the same policy, reading it from the base branch. Branch protection makes that check required.

## Git invocation checklist for M3/M4 (planned)

Nothing runs git yet. When `internal/vcs` (M3) and `regreen verify` (M4) do, they must produce the diff like this. Each flag and setting below was checked against `git diff -h` and `git help diff` / `git help config` (git 2.39.5).

1. Run from the repository root and pass `--no-relative`. `diff.relative` (or `--relative`) makes git show only changes under the current directory, with shortened paths; `--no-relative` countermands both.
2. Pass `--ignore-submodules=none`. With `diff.ignoreSubmodules=all`, `submodule.<name>.ignore=all` in `.git/config`, or `ignore = all` in `.gitmodules`, git leaves a submodule pointer change out of the diff entirely; `none` overrides all three. (Checked in a throwaway repository: without the flag, a commit that bumps a submodule shows no change for it.)
3. Pin the format settings with `-c`: `-c core.quotepath=off -c diff.suppressBlankEmpty=false -c diff.noprefix=false -c diff.mnemonicPrefix=false`. Pin the prefixes with `--src-prefix=a/ --dst-prefix=b/`: git 2.39.5 documents no config key for the prefixes, so they cannot be pinned with `-c` there, and the command-line flags work regardless of configuration. Keep `--no-color --no-ext-diff --no-textconv --no-renames`.
4. Treat repository-local git configuration as hostile. The agent works in the same checkout and can write `.git/config` directly (it never appears in a diff) and can edit `.gitmodules` (protected in diffs, but the working copy is what git reads). Some settings only reformat the output and make the parser fail; others, such as the submodule `ignore` settings and `diff.relative` above, make git omit changes, which the parser cannot notice. Override every such setting with an explicit command-line flag rather than trusting the configuration.
5. Cap the size of git's output before parsing it, and treat output over the cap as a rejection.
6. Consider the plumbing command `git diff-tree -r -p <base> <head>`, which porcelain settings such as `diff.renames` do not affect. It still honors `.gitmodules` `ignore = all` by default (checked), so it needs `--ignore-submodules=none` and the other flags too.

## Trust model (M1 to M4)

M1 delivered two parts of this design: the always-protected paths and the policy engine. Everything else below is planned and is labelled with its milestone.

The fixing agent is untrusted. regreen believes the git diff and the result of re-running the checks, and nothing the agent says about its own work. Local protections (the MCP tool flow, the pre-push hook) catch problems early; the enforcement that cannot be bypassed locally is the CI check plus branch protection.

- An agent could change `.github/**` or `regreen.yml` in a diff. Those paths are therefore hardcoded as protected, and the policy rejects a diff that touches them. **Built (M1).** The list is inside `internal/policy`, `Evaluate` applies it whenever it judges a diff (with invalid rules it rejects the diff before checking any path), and no config key or caller option removes it. Config can only add protected patterns.
- The policy limits (changed and deleted lines, edit scope, forbidden patterns, attempts) are enforced by `Evaluate`. **Built (M1)**, but nothing calls it yet.
- `regreen verify` reads the policy from the base branch, never from the pushed commit. **Planned (M4).** M1 only makes it possible: `config.ParseRepo` takes bytes.
- Attempts are counted from commit trailers in git history, not from a state file. **Planned (M3).** M1 takes the count as an input number.
- Maintainers make the verify check required in branch protection, so removing it blocks merging. **Planned (M4)**, as documentation and `regreen init`.
- A human reviews before merge. regreen never merges.

Always-protected paths, hardcoded and not configurable (the full list, with how they match, is in [CONFIG.md](CONFIG.md)): `regreen.yml`, `regreen.yaml`, `.regreen`, `.github`, `.git`, `.gitmodules`, and the git-hook and pre-commit configuration paths `.githooks`, `.husky`, `.pre-commit-config.yaml`, `lefthook.yml` and `.lefthook.yml`.

These controls guarantee that a fix follows the rules, not that it is logically correct.
