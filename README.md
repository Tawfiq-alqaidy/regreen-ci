# regreen-ci

regreen-ci hands mechanical CI failures (lint, formatting, type errors) to your own coding agent on your own machine, and validates the fix against strict rules before anything is pushed. The CLI binary is named `regreen`.

A failing CI is CI doing its job. regreen does not try to make the pipeline green at any cost. It removes the wasted human time on failures that need no human judgment, without weakening the checks.

## Status

Early development. Milestone M0 (scaffold) is complete: the binary builds and prints help and its version. Nothing else works yet.

| Milestone | Delivers | State |
|---|---|---|
| M0 | Scaffold: module, CLI skeleton, lint, CI, docs | done |
| M1 | Config loading and the policy engine | planned |
| M2 | GitHub Actions adapter and the failure model | planned |
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

Requires Go 1.27.4 or newer.

```sh
make build          # writes bin/regreen
./bin/regreen --help
./bin/regreen version
make check          # vet, lint, tests
```

## Documentation

- [Architecture](docs/ARCHITECTURE.md)
- [Decisions log](docs/DECISIONS.md)

## License

[Apache-2.0](LICENSE)
