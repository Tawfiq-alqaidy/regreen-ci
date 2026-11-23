# Decisions log

Each entry records what was decided, the alternatives, and why. Newest entries go at the bottom. Entries are not rewritten; a later decision that changes an earlier one gets its own entry and links back.

## D1. License: Apache-2.0

- **Date:** 2026-11-23 (M0)
- **Decision:** Apache-2.0.
- **Alternatives:** MIT.
- **Why:** Chosen by the maintainer. Permissive like MIT, with an explicit patent grant.

## D2. Module path: `github.com/Tawfiq-alqaidy/regreen-ci`

- **Date:** 2026-11-23 (M0)
- **Decision:** The module path matches the git remote exactly, including case.
- **Alternatives:** A lowercased owner name.
- **Why:** Chosen by the maintainer. `go install github.com/Tawfiq-alqaidy/regreen-ci/cmd/regreen@latest` works without a redirect.

## D3. Minimum Go version: latest stable only (1.27.4)

- **Date:** 2026-11-23 (M0)
- **Decision:** Support only the current stable Go release. The `go` directive in `go.mod` is the single source of the version; CI reads it with `go-version-file`.
- **Alternatives:** Supporting the latest two releases, as Go's own support policy does.
- **Why:** Chosen by the maintainer. A new project has no users on older toolchains, and one version keeps CI simple.

## D4. CLI on the standard library, no CLI framework

- **Date:** 2026-11-23 (M0)
- **Decision:** Dispatch subcommands with a plain `switch` in `run`. No cobra, urfave/cli, or kong.
- **Alternatives:** cobra (the most common Go CLI framework).
- **Why:** The brief asks for few dependencies, each justified. regreen has a handful of subcommands with few flags, which the standard library handles. If flag handling grows past what `flag.FlagSet` per subcommand does comfortably, revisit this.
- **Cost if wrong:** Moving to cobra later means rewriting `cmd/regreen/app.go` and its tests; nothing under `internal/` depends on it.

## D5. `run(args, stdout, stderr) int` instead of logic in `main()`

- **Date:** 2026-11-23 (M0)
- **Decision:** `main()` only calls `run` and exits with its return value. `run` receives its output streams as parameters.
- **Alternatives:** Logic in `main()` tested by building and executing the binary.
- **Why:** In-process tests are fast and can assert exactly what goes to stdout and what goes to stderr. That separation is a hard requirement for `regreen mcp`, where stdout carries the protocol.

## D6. Version from ldflags, then Go build info

- **Date:** 2026-11-23 (M0)
- **Decision:** `regreen version` reports the value injected with `-ldflags "-X main.version=..."`, else the module version from `runtime/debug.ReadBuildInfo`, else `dev`. The build-info version is the release tag for `go install module@version`, and a pseudo-version naming the commit (for example `v0.0.0-<date>-<commit>+dirty`) for a local `go build` in a git checkout.
- **Alternatives:** ldflags only; build info only.
- **Why:** Release builds control the string through ldflags, and users who install with `go install` still see a real version without any release tooling. Local builds are left with the toolchain's pseudo-version instead of being forced to `dev`, because it says exactly which commit the binary came from and whether the tree was modified.

## D7. Linting with golangci-lint v2.15.0

- **Date:** 2026-11-23 (M0)
- **Decision:** golangci-lint v2 with the `standard` linter set plus errorlint, gocritic, gosec, misspell, revive, unconvert, and unparam; gofmt and goimports as formatters. The version is pinned in the `Makefile` and repeated in `.github/workflows/ci.yml`; the two must be changed together.
- **Alternatives:** `go vet` and `staticcheck` run separately; an unpinned "latest" linter.
- **Why:** One tool, one config file, the same result locally and in CI. Pinning stops a new linter release from failing CI on an unrelated commit. gosec is included because regreen runs commands and handles tokens. The linter is a development tool run with `go run`, not a dependency in `go.mod`.

## D8. GitHub Actions pinned to commit SHAs

- **Date:** 2026-11-23 (M0)
- **Decision:** Every `uses:` in the workflow references a full commit SHA with the tag in a trailing comment. The workflow's token is limited to `contents: read`.
- **Alternatives:** Pinning to a major tag such as `@v4`.
- **Why:** Tags can be moved; a SHA cannot. regreen is a tool about not weakening CI, so its own CI should not be changeable from outside the repository.
