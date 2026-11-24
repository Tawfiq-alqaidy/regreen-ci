# regreen-ci

regreen-ci hands mechanical CI failures (lint, formatting, type errors) to your own coding agent on your own machine, and validates the fix against strict rules before anything is pushed. The CLI binary is named `regreen`.

A failing CI is CI doing its job. regreen does not try to make the pipeline green at any cost. It removes the wasted human time on failures that need no human judgment, without weakening the checks.

## Status

Early development. Milestones M0 to M2 are done. M0 is the scaffold: the binary builds and prints help and its version. M1 loads `regreen.yml` and the user config and decides whether a diff follows the rules. M2 adds a GitHub Actions adapter (the CI result for a commit, and the log of a failed step) and a failure model (a trimmed, sanitized excerpt, the known files it names, and the configured check it maps to). All of it is tested library code: no command uses it yet, so the binary still does nothing beyond help and version.

| Milestone | Delivers | State |
|---|---|---|
| M0 | Scaffold: module, CLI skeleton, lint, CI, docs | done |
| M1 | Config loading and the policy engine | done |
| M2 | GitHub Actions adapter and the failure model | done, pending review |
| M3 | MCP server (`regreen mcp`) | planned |
| M4 | Enforcement: `regreen verify`, `regreen init`, pre-push hook | planned |
| M5 | Presets: JavaScript/TypeScript, Laravel, Flutter | planned |
| M6 | Watcher (`regreen watch`) | planned |

## Principles

- **Never trust the agent.** The only truth is the git diff and the result of re-running the check.
- **Deterministic first, agent second.** If the project's own autofix command resolves the failure, no agent is involved.
- **Language-agnostic core.** Projects declare check and fix commands in config. Languages are supported through presets, not Go code.
- **The final guarantee lives where the agent cannot edit it:** a CI check plus branch protection.
- **A human approves what reaches the main branch.** regreen never merges.

These controls guarantee that a fix follows the rules, not that it is logically correct. Human review before merge is part of the design.

## Build from source

Requires the Go version in the `go` directive of `go.mod`.

```sh
make build          # writes bin/regreen
./bin/regreen --help
./bin/regreen version
make check          # vet, lint, tests
```

## Documentation

- [Architecture](docs/ARCHITECTURE.md)
- [Configuration reference](docs/CONFIG.md)
- [Decisions log](docs/DECISIONS.md)
- [Milestone M0 summary](docs/milestones/M0.md)
- [Milestone M1 summary](docs/milestones/M1.md)
- [Milestone M2 summary](docs/milestones/M2.md)

## License

[Apache-2.0](LICENSE)
