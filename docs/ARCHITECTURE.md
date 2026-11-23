# Architecture

regreen is one binary with several subcommands over one shared core. This document describes the intended layout and marks what exists today. It is updated at the end of every milestone.

## What exists today (M0)

Every statement in this section must be checked against the code at the end of each milestone.

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

## Planned layout

| Package | Responsibility | Milestone |
|---|---|---|
| `cmd/regreen/` | Entry point and subcommand wiring | M0 |
| `internal/config/` | Repo config (`regreen.yml`) and user config: loading and validation | M1 |
| `internal/policy/` | The policy engine. Pure functions, no I/O: takes a diff and a rule set, returns a verdict with reasons | M1 |
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

## Trust model (planned, M1 to M4)

Nothing in this section exists yet. It describes the design the milestones will build.

The fixing agent is untrusted. regreen believes the git diff and the result of re-running the checks, and nothing the agent says about its own work. Local protections (the MCP tool flow, the pre-push hook) catch problems early; the enforcement that cannot be bypassed locally is the CI check plus branch protection.

- An agent could change `.github/**` or `regreen.yml` in a diff. Those paths are therefore hardcoded as protected, and the policy rejects a diff that touches them.
- `regreen verify` reads the policy from the base branch, never from the pushed commit.
- Maintainers make the verify check required in branch protection, so removing it blocks merging.
- A human reviews before merge. regreen never merges.

Always-protected paths, hardcoded and not configurable: `regreen.yml`, `.github/**`, `.regreen/**`, and git hooks.

These controls guarantee that a fix follows the rules, not that it is logically correct.
