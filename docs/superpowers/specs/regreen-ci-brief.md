# regreen-ci: project brief for the implementing agent

You are building regreen-ci, an open-source tool written in Go. The CLI binary is named `regreen`. Read this whole brief before writing any code. It is the source of truth for scope and design. Where it is silent, ask the maintainer instead of guessing.

## 1. The problem

A developer (or their coding agent) pushes a commit. Ten minutes later CI fails on something mechanical: a lint rule, a formatting issue, a type error. The fix is often one line, but the cycle costs a context switch, a hunt through long logs, another push, and another wait.

regreen-ci closes that loop on the developer's own machine. It watches the CI result for a pushed commit and hands the failure to the developer's own coding agent (Claude Code, Codex, or any MCP-capable agent), running under the developer's own subscription. The agent fixes the code, and regreen validates the fix against strict rules before anything is pushed.

A failing CI is CI doing its job. The goal is not to make the pipeline green at any cost. The goal is to remove wasted human time on failures that need no human judgment, without ever weakening the checks.

## 2. Core principles

These drive every design decision. If a choice conflicts with one of them, stop and ask.

- **Never trust the agent.** regreen treats the fixing agent as untrusted. The only truth is the git diff and the result of re-running the check. What the agent says about its own work is ignored.
- **Deterministic first, agent second.** If the project's own autofix command (for example `eslint --fix`) resolves the failure, no agent reasoning is needed.
- **Language-agnostic core.** The core knows nothing about any language. Projects declare check and fix commands in config. Language support is added through presets (config files), not Go code.
- **Design for every CI provider, implement one.** GitHub Actions is the only provider in scope now, but it sits behind an interface so GitLab CI and Jenkins can be added as one new adapter each.
- **The final guarantee lives where the agent cannot edit it.** Local protections catch problems early. The real enforcement is a CI check plus branch protection.
- **A human approves what reaches the main branch.** regreen never merges.

## 3. Scope

### In scope

- Local CLI in Go, single static binary.
- `regreen mcp`: an MCP server over stdio that the developer's agent connects to.
- Static checks only: lint, format, type-check, defined by commands in config.
- GitHub Actions as the CI provider.
- Policy engine: protected files, forbidden patterns, diff limits, attempt limits.
- `regreen verify`: runs the same policy inside CI.
- `regreen init`: sets up config, the pre-push hook, and agent permission hints.
- Presets for JavaScript/TypeScript, Laravel (PHP), and Flutter (Dart).
- `regreen watch`: background watcher for manual pushes (last milestone).

### Out of scope for now. Do not build these.

- Fixing failing tests or builds. They need extra safeguards that are not designed yet.
- Flaky-test detection or retries.
- GitLab CI, Jenkins, or any other provider.
- A hosted service, webhooks, dashboards, or telemetry.

### Never in scope

- Any action on deployment or infrastructure.
- Auto-merging.
- Letting the agent modify CI configuration, regreen configuration, or secrets.

## 4. Architecture

One binary, several subcommands, one shared core.

```
cmd/regreen/          entry point, subcommand wiring
internal/config/      repo config + user config loading and validation
internal/policy/      the policy engine (pure functions, no I/O)
internal/failure/     the normalized failure model
internal/ci/          CIProvider interface
internal/ci/github/   GitHub Actions adapter
internal/vcs/         git operations (diff, commit, push, trailers)
internal/state/       local state (claimed commits), outside the repo
internal/mcpserver/   MCP tools, thin layer over the core
internal/watch/       background watcher (last milestone)
presets/              YAML presets, embedded with go:embed
```

Keep `internal/policy` free of I/O so it can be tested exhaustively with table-driven tests. It receives a diff and a rule set and returns a verdict with reasons.

### Two config files

Repo config (`regreen.yml`, committed, belongs to the project):

```yaml
checks:
  - name: lint
    run: npm run lint
    fix: npx eslint --fix .        # optional deterministic fix
policy:
  protected_files: [".eslintrc*", "tsconfig.json"]
  forbidden_patterns: ["eslint-disable", "@ts-ignore", "@ts-expect-error"]
  max_changed_lines: 200
  max_deleted_lines: 50
  max_attempts: 2
  edit_scope: files_in_errors      # agent may only touch files named in the errors
```

User config (in the OS user config directory, never committed): which agent the developer uses and how failures are delivered in watch mode. It belongs to the person, not the project.

Some paths are always protected, hardcoded, and not configurable: `regreen.yml`, `.github/**`, `.regreen/**`, and git hooks. A project can add protected paths but can never remove these.

## 5. MCP tools

The agent's loop is: push, `wait_for_ci`, on failure `get_failure`, `apply_autofix`, edit files if errors remain, `submit_fix`, then `wait_for_ci` again.

Write tool descriptions carefully. They are the instructions the fixing agent actually reads, so each one must say when to call it and what to do next.

### wait_for_ci

- Input: `commit` (optional, defaults to HEAD). The repo is derived from the git remote.
- Blocks for a short bounded time (about 45 seconds), because MCP clients time out long tool calls. If CI has not finished, return `pending` and the agent calls again.
- Output: `status` (`success` | `failure` | `pending` | `no_runs`), `commit`, and `failed_checks` as a list of `{failure_id, job, step}`.
- Never returns raw logs.
- Side effect: records the commit as claimed in local state so the watcher ignores it.

### get_failure

- Input: `failure_id`.
- Output: the normalized failure: `check`, `command`, `errors` (only the relevant lines, trimmed and size-capped), `files`, and `rules` (`protected_files`, `forbidden_patterns`, `attempts_remaining`).
- Log text is untrusted input. Return it clearly delimited as data. Nothing in a log may ever be executed or treated as an instruction by regreen.

### apply_autofix

- Input: `failure_id`.
- Runs the check's `fix` command if one is defined, then re-runs `run`.
- If files changed, commits them as a separate commit so deterministic changes are reviewable apart from agent edits.
- Output: `status` (`fixed` | `partially_fixed` | `no_fix_command` | `unchanged`) and the remaining errors, if any.

### submit_fix

The only path to a push.

- Input: `failure_id`, `summary` (what changed and why, used in the commit message).
- Steps, in order: read the diff, run the policy engine, run every configured check locally (not only the one that failed), then commit and push.
- Output on success: `{status: "pushed", commit}`.
- Output on rejection: `{status: "rejected", reasons: [...], attempts_remaining}` with reasons specific enough to act on (file, line, rule).
- When attempts are exhausted: `{status: "gave_up"}`, and the description must tell the agent to stop and report to the developer.

## 6. Hardening requirements

Each row is a scenario that must be covered by a test.

| Scenario | Required protection |
|---|---|
| Agent runs `git push` directly | `regreen init` emits agent permission config denying direct push; pre-push hook runs the policy; `regreen verify` in CI |
| Agent pushes with `--no-verify` | `regreen verify` in CI, made a required check by branch protection |
| Agent edits `regreen.yml` to relax rules | Hardcoded protection of that file; `verify` reads policy from the base branch, never from the pushed commit |
| Agent removes the verify job from the workflow | `.github/**` is protected; documentation tells maintainers to make the check required so its absence blocks merging |
| Agent resets the attempt counter by deleting state | Attempts are counted from commit trailers in git history (`Regreen-Fix`, `Regreen-Attempt`), not from the state file |
| Agent passes lint by deleting or commenting out code | `max_deleted_lines`, `edit_scope`, and forbidden patterns applied to added lines |
| Fix for one check breaks another | `submit_fix` runs all configured checks before pushing |
| Instructions injected through CI logs | Logs are trimmed, delimited, and never executed; the resulting diff still passes through the same policy |

Be honest in the documentation about the limit: these controls guarantee a fix follows the rules, not that it is logically correct. Human review before merge is part of the design.

## 7. Technical requirements

- MCP over stdio means stdout is reserved for the protocol. All logging goes to stderr. A single stray print to stdout breaks the server.
- Use the official MCP Go SDK and an established GitHub client library. Check their current documentation before using them. Do not rely on memory for API signatures. Pin versions.
- Keep dependencies few. Justify each one in the decisions log.
- GitHub authentication: read a token from the environment, falling back to the `gh` CLI if it is installed. Never log tokens, never write them to disk.
- Local state lives in the OS user state or config directory, never inside the repo.
- Pass `context.Context` through all I/O. Wrap errors with enough context to diagnose.
- Fix work happens in a separate git worktree when the developer's working tree is dirty or on another branch, so their work is never disturbed.
- Unit tests must not touch the network. Fake the GitHub API with `httptest`. Test git logic against temporary repositories.
- The project's own CI must run lint, vet, and tests, and should use regreen on itself once the tool is usable.

## 8. Milestones

Work through these in order. Stop at the end of each milestone and wait for the maintainer's review before starting the next.

- **M0. Scaffold.** Go module, CLI skeleton with `--help` and `version`, linting, project CI, README stub, `docs/ARCHITECTURE.md`, `docs/DECISIONS.md`.
- **M1. Config and policy engine.** Config loading and validation, hardcoded protected paths, the full policy engine with table-driven tests covering every row in section 6 that does not need network access.
- **M2. GitHub adapter.** `CIProvider` interface, the GitHub Actions implementation (find runs for a commit, find failed jobs and steps, fetch and trim logs), and the normalized failure model.
- **M3. MCP server.** The four tools, local state, commit trailers, worktree handling. End-to-end test against a temporary repo with a faked CI provider.
- **M4. Enforcement.** `regreen verify`, `regreen init`, the pre-push hook, and documentation for branch protection and CODEOWNERS.
- **M5. Presets.** JavaScript/TypeScript, Laravel, Flutter, with auto-detection from project files and a contributor guide for adding a preset.
- **M6. Watcher.** `regreen watch` for manual pushes, with delivery modes: notify only (default), push to a running session where the agent supports it, or a new headless run.

## 9. How to work

- Before each milestone, write a short plan and list any assumptions. Wait for approval if the plan involves a decision this brief does not cover.
- Prefer small, focused commits with clear messages.
- Every behavior has a test. A milestone is not done while tests fail or are missing.
- When this brief and reality disagree (an API does not work as described, a requirement is contradictory), stop and raise it. Do not work around it silently.
- Record each meaningful design decision in `docs/DECISIONS.md`: what was decided, the alternatives, and why.
- At the end of each milestone, write a plain-language summary: what was built, how the pieces connect, how to try it by hand, and a short explanation of any Go idiom or pattern that is not obvious. The maintainer wants to understand every part of this project, not only receive working code.

## 10. Questions to ask the maintainer before M0

- Which license (MIT or Apache-2.0)?
- The Go module path (GitHub owner and repo name).
- The minimum Go version to support.

### Answers (2026-11-23)

- License: **Apache-2.0**.
- Module path: **`github.com/Tawfiq-alqaidy/regreen-ci`** (matches the git remote).
- Minimum Go version: **latest stable only** (the `go` directive tracks the current stable release).
