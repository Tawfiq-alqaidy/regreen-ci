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
  protected.go  the always-protected path list; IsProtected, AlwaysProtected
  parse.go      ParseUnifiedDiff: patch text from git -> Diff, failing closed
  policy.go     Rules, Input, Verdict, Reason, the Rule* names; Evaluate

internal/config/        reads files and turns them into policy.Rules
  repo.go       ParseRepo, LoadRepo: regreen.yml -> Repo (checks + policy.Rules)
  user.go       LoadUser, UserConfigPath: the per-user config file
```

`internal/policy` may import only the standard library. A test (`TestPolicyPackageIsPure`) reads the imports of every non-test file and fails if one of them is a third-party package, a package of this module, or a package that does I/O (`os`, `os/exec`, `io/fs`, `net`, `time` and similar). `internal/config` imports `internal/policy`, never the other way round.

Data flow:

```
regreen.yml bytes --config.ParseRepo--> config.Repo{Checks, Policy policy.Rules}
patch text        --policy.ParseUnifiedDiff--> policy.Diff
policy.Input{Diff, Rules, ErrorFiles, AttemptsUsed} --policy.Evaluate--> policy.Verdict
```

`ParseRepo` takes bytes, not a path, so a caller that must not trust the working tree can pass bytes it read from the base branch (`regreen verify`, M4). `LoadRepo` and `LoadUser` read a file and call the same parsing code.

`Evaluate` takes four things and nothing else: the diff, the rules, the files named in the failure's errors, and the number of attempts already used. There is no input for anything the agent says about its own work, and no state file. It reports every violation it finds, as a list of `Reason` values, each naming a rule from a fixed set. The order is:

1. Rules that are themselves invalid (`invalid_rules`): a limit out of range, an unknown `edit_scope`, an invalid `protected_files` pattern, an empty forbidden pattern, a negative attempt count. If there are any, `Evaluate` returns only these reasons and stops.
2. `max_attempts`: attempts used is not below the limit.
3. `empty_diff`: the diff changes no files.
4. For each file, in the order the patch lists them:
   1. `invalid_path`, or `invalid_diff` for a malformed `Diff` value (a negative deletion count, a duplicate path, an unknown change kind, a binary change with line counts, an added line numbered below 1). If either applies, the remaining per-file checks are skipped for that file.
   2. `protected_file`: the always-protected list, then `protected_files` from the config.
   3. `edit_scope`: with `files_in_errors`, the file must be named in the errors.
   4. `binary_file`.
   5. `special_file`: symlink, submodule, or mode change.
   6. `forbidden_pattern`: one reason per added line and pattern that matches. At most 20 of these are returned; one more reason says how many were left out.
5. `max_deleted_lines`, then `max_changed_lines` (added plus deleted), over the whole diff.

The diff is allowed exactly when the list of reasons is empty. `AttemptsRemaining` is `max(0, max_attempts - attempts used)` and is reported whether or not the diff is allowed.

The diff parser expects the output of `git -c core.quotepath=off diff --no-color --no-ext-diff --no-textconv --no-renames <base> <head>` and rejects anything it does not recognize, because the engine judges only what the parser reports. The code that runs git (`internal/vcs`, M3) must also pin the git settings that change the output format: `diff.suppressBlankEmpty` (off), `diff.noprefix` (off), and the source and destination prefixes (`a/` and `b/`). The parser rejects output that departs from these, which fails closed but would make `regreen` unusable on a machine with such a setting if M3 forgot to pin them.

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

## Trust model (M1 to M4)

M1 delivered two parts of this design: the always-protected paths and the policy engine. Everything else below is planned and is labelled with its milestone.

The fixing agent is untrusted. regreen believes the git diff and the result of re-running the checks, and nothing the agent says about its own work. Local protections (the MCP tool flow, the pre-push hook) catch problems early; the enforcement that cannot be bypassed locally is the CI check plus branch protection.

- An agent could change `.github/**` or `regreen.yml` in a diff. Those paths are therefore hardcoded as protected, and the policy rejects a diff that touches them. **Built (M1).** The list is inside `internal/policy`, `Evaluate` applies it on every call, and no config key or caller option removes it. Config can only add protected patterns.
- The policy limits (changed and deleted lines, edit scope, forbidden patterns, attempts) are enforced by `Evaluate`. **Built (M1)**, but nothing calls it yet.
- `regreen verify` reads the policy from the base branch, never from the pushed commit. **Planned (M4).** M1 only makes it possible: `config.ParseRepo` takes bytes.
- Attempts are counted from commit trailers in git history, not from a state file. **Planned (M3).** M1 takes the count as an input number.
- Maintainers make the verify check required in branch protection, so removing it blocks merging. **Planned (M4)**, as documentation and `regreen init`.
- A human reviews before merge. regreen never merges.

Always-protected paths, hardcoded and not configurable (the full list, with how they match, is in [CONFIG.md](CONFIG.md)): `regreen.yml`, `regreen.yaml`, `.regreen`, `.github`, `.git`, and the git-hook and pre-commit configuration paths `.githooks`, `.husky`, `.pre-commit-config.yaml`, `lefthook.yml` and `.lefthook.yml`.

These controls guarantee that a fix follows the rules, not that it is logically correct.
