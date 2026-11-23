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

## D9. Planned commands are listed in help and exit 1 until built

- **Date:** 2026-11-23 (M0)
- **Decision:** `mcp`, `verify`, `init`, and `watch` appear in `regreen help` now, each marked "(planned)". Running one prints "not implemented yet (planned for milestone Mx)" to stderr and exits 1. Each command leaves `plannedCommands` (and loses its "(planned)" marker) when its milestone lands.
- **Alternatives:** Ship only `help` and `version`, and describe the rest in the README.
- **Why:** Approved by the maintainer. Help shows the intended shape of the tool, and a test keeps the help text and the command table in step.

## D10. Exit codes: 0 success, 1 error, 2 usage error

- **Date:** 2026-11-23 (M0)
- **Decision:** Every subcommand returns one of these three codes. 2 means the invocation itself was wrong (no command, unknown command). 1 means the command ran and failed, or is not built yet.
- **Alternatives:** A single non-zero code; per-failure codes.
- **Why:** Scripts, hooks, and CI (`regreen verify` in M4) need to tell "you called it wrong" from "the check failed". Three codes is the common Unix convention and is enough until a concrete need appears.

## D11. YAML library: `go.yaml.in/yaml/v4` v4.0.0

- **Date:** 2026-11-23 (M1)
- **Decision:** Parse YAML with `go.yaml.in/yaml/v4`, pinned at v4.0.0 in `go.mod`. This is the project's first dependency, and the only one so far. The golangci-lint tool is not counted: it runs through `go run` and is not in `go.mod` (D7).
- **Alternatives:** `github.com/goccy/go-yaml`; `gopkg.in/yaml.v3`, which is archived and no longer maintained.
- **Why:** Chosen by the maintainer. It is the maintained fork of `yaml.v3`, so it keeps the API most Go programmers already know, and it can reject unknown keys while decoding (`KnownFields(true)`), which D12 needs. It also exposes the parsed document tree (`yaml.Node`), which D23 needs to check the YAML type of a value.
- **Cost if wrong:** Only `internal/config` imports it. Replacing it means rewriting the decoding half of that package; `internal/policy` is unaffected.

## D12. Strict config loading

- **Date:** 2026-11-23 (M1)
- **Decision:** Config files are decoded strictly. An unknown key is an error. A file must hold exactly one YAML document. A file over 1 MiB is rejected (a file is read with a limit, so an oversized one is detected without loading all of it). Every validation problem found is reported in one error (`errors.Join`), each prefixed with the file name, instead of stopping at the first.
- **Alternatives:** Ignore unknown keys (the YAML library's default); stop at the first problem.
- **Why:** A misspelled key such as `forbiden_patterns` would otherwise silently leave a protection switched off, and the tool exists to enforce rules. Reporting all problems at once saves repeated edit-and-run cycles. The size cap and the single-document rule keep the loader from being fed something huge or ambiguous.
- **Cost if wrong:** A key added in a later version is an error for users on an older binary. That is the intended direction (fail rather than ignore), but it means new keys need a release before they appear in a shared `regreen.yml`.

## D13. Strict defaults for omitted policy keys

- **Date:** 2026-11-23 (M1)
- **Decision:** An omitted key takes a strict default: `max_changed_lines: 200`, `max_deleted_lines: 50`, `max_attempts: 2`, `edit_scope: files_in_errors`. Omitted `protected_files` and `forbidden_patterns` mean no extra entries. A whole omitted `policy:` section means all defaults. Explicit values must be in range (1 to 1,000,000 for `max_changed_lines` and `max_attempts`; 0 to 1,000,000 for `max_deleted_lines`).
- **Alternatives:** Require every key to be written; treat an omitted limit as unlimited.
- **Why:** Chosen by the maintainer. A config that says nothing should still be safe, and a minimal `regreen.yml` should work. "Unlimited when omitted" would make forgetting a key the same as removing a protection.
- **Cost if wrong:** The defaults may be too tight for some projects (a large mechanical fix over 200 lines is rejected until the limit is raised). That is a visible failure with a clear message, which was judged better than a silent weakness.

## D14. `forbidden_patterns` are literal substrings, checked on added lines only

- **Date:** 2026-11-23 (M1)
- **Decision:** Each entry is a literal, case-sensitive substring. It is checked only against lines the diff adds, never against lines it removes or leaves as context. An entry may not be empty or contain a newline.
- **Alternatives:** Regular expressions; literal by default with an opt-in `re:` prefix for regular expressions.
- **Why:** Chosen by the maintainer. Literals cannot be written wrongly in a way that silently matches nothing, they cannot hang the matcher, and they are easy to explain. Checking only added lines means the rule says "do not introduce this", so removing an existing suppression comment is always allowed, which is what a fix for a lint error often needs.
- **Cost if wrong:** Literals are easy to evade by rewording (for example a linter's second suppression syntax), and case differences are not matched. The documents say so, and human review before merge remains part of the design. Adding an opt-in `re:` prefix later would not break existing configs.

## D15. `protected_files` use gitignore-style patterns and an in-repo matcher

- **Date:** 2026-11-23 (M1)
- **Decision:** `protected_files` entries are gitignore-style patterns (syntax in `docs/CONFIG.md`), matched by a short matcher in `internal/policy/glob.go` built on `path.Match` from the standard library, with no dependency. A pattern that matches a directory covers everything under it. A leading `!` (negation) is rejected, so configuration can only add protection, never carve out an exception. Malformed patterns are errors.
- **Alternatives:** A third-party gitignore library; plain `path.Match` globs (no `**`, no directory coverage); plain prefixes.
- **Why:** Chosen by the maintainer. People already know gitignore patterns. Writing the matcher in this package keeps the engine free of dependencies and lets the same code validate patterns (`ValidatePattern`) and match them (`MatchPath`), so the two cannot disagree. Rejecting negation closes a way to weaken the always-protected list.
- **Cost if wrong:** The matcher supports a subset of gitignore. Patterns gitignore accepts but this does not (such as a backslash escape) are rejected with an error, not silently misread.

## D16. The always-protected list

- **Date:** 2026-11-23 (M1)
- **Decision:** These patterns are hardcoded in `internal/policy/protected.go`: `regreen.yml`, `regreen.yaml`, `.regreen`, `.github`, `.git`, `.githooks`, `.husky`, `.pre-commit-config.yaml`, `lefthook.yml`, `.lefthook.yml`. Each has no slash, so it matches at any depth (`pkg/regreen.yml` is protected), and each covers everything under it when it names a directory. They match case-insensitively, so `.GitHub/` and `Regreen.yml` are protected too. `Evaluate` applies them on every call; no `Rules` value, config key or option removes them. The brief names "git hooks" without listing them: `.githooks`, `.husky`, `.pre-commit-config.yaml`, `lefthook.yml` and `.lefthook.yml` are this project's reading of that phrase (the common places where hook scripts or hook-manager configuration live), and the list can be extended in a later milestone.
- **Alternatives:** Only the three paths the brief names; make the list configurable.
- **Why:** A configurable safety list could be edited away by the same agent it constrains. Case-insensitive matching covers case-insensitive file systems (macOS and Windows by default), where `.GitHub/workflows/ci.yml` is the same file as `.github/workflows/ci.yml`.
- **Cost if wrong:** A legitimate fix that must change one of these files is always rejected, and a human must make it. Hook tools not on the list (for example a different hook manager) are not protected until they are added.

## D17. The diff parser lives in `internal/policy`, built in M1

- **Date:** 2026-11-23 (M1)
- **Decision:** `ParseUnifiedDiff` is in `internal/policy`, although the brief lists git operations under `internal/vcs` in M3. It is a pure function from patch text to `Diff`. It accepts only the output of `git diff --no-renames` with the flags listed in its doc comment, and it returns an error for anything it does not recognize.
- **Alternatives:** Build the parser with the git code in M3 and test the engine against hand-built `Diff` values until then.
- **Why:** The section 6 hardening tests need real patches, so the parser had to exist in M1 to test the engine honestly. The parser decides what the engine sees, so a parser the policy package does not own and test together with it is a bypass risk: a misreading there is a hole in the policy. `--no-renames` makes every change a plain add, delete or modify of one path, so each path can be checked against the protected list on its own; a rename, which names two paths, is rejected.
- **Cost if wrong:** M3 must produce exactly this input format (see D22). Code that runs git lives in `internal/vcs` and passes text to the parser; if the parser needs git behavior the engine's rules do not cover, the boundary between the packages will need to be revisited.

## D18. Binary files, symlinks, submodules and mode changes are rejected

- **Date:** 2026-11-23 (M1)
- **Decision:** A binary change (`binary_file`) or a symlink, submodule or file-mode change (`special_file`) rejects the fix, on any path, whatever the rest of the rules say.
- **Alternatives:** Allow them and count them as zero lines; allow them under configuration.
- **Why:** The engine reasons about lines of text. A binary file has no lines to count, so it would bypass the line limits and the forbidden patterns. A symlink or a submodule pointer can redirect a path to somewhere the rules never looked, and a mode change (for example making a file executable) is not a line change either. Fail closed: what the engine cannot measure, it does not allow.
- **Cost if wrong:** A fix that regenerates a binary asset, or makes a script executable, is always rejected and needs a human.

## D19. Attempts are an input number, not state

- **Date:** 2026-11-23 (M1)
- **Decision:** `Input.AttemptsUsed` is a number the caller passes in. `Evaluate` has no state file, no clock and no way to read either. M3 will count attempts from the `Regreen-Fix` and `Regreen-Attempt` commit trailers in git history.
- **Alternatives:** Keep a counter in a state file that the engine reads and updates.
- **Why:** An agent that can delete or edit a state file could reset its own attempt limit. A test (`agent resets the attempt counter by deleting state`) also fixes the exact list of `Input` fields, so adding a new input for the agent to influence needs a deliberate change to the test.
- **Cost if wrong:** Correct attempt counting is now entirely the caller's job. M1 only checks the arithmetic; the trailer counting itself is not built or tested until M3.

## D20. The user config shape is provisional

- **Date:** 2026-11-23 (M1)
- **Decision:** The user config (`agent`, `watch.delivery`; see `docs/CONFIG.md`) is loaded and validated now, but its shape may change in M6, which is the first milestone that uses it. A missing or empty file means defaults (`notify`, no agent).
- **Alternatives:** Leave the user config out until M6.
- **Why:** The brief puts config loading in M1, and building it now settles the strict-loading code that both config files share. Nothing depends on the keys yet.
- **Cost if wrong:** If M6 needs a different shape, the keys change and a user who already wrote the file gets an error naming the unknown key. Nothing else is affected.

## D21. `Evaluate` rejects malformed input instead of trusting it

- **Date:** 2026-11-23 (M1, added after review)
- **Decision:** `Evaluate` does not assume its `Diff` and `Rules` come from the parser and the config loader. It adds three rules beyond the ones in the brief: `invalid_path` (the first per-file check: an empty path, an absolute path, `..`, a backslash, a NUL byte, or a path that is not already clean), `invalid_diff` (a `FileChange` that git could not have produced: a negative deletion count, a duplicate path, an unknown change kind, a binary change with line counts, an added line numbered below 1) and `invalid_rules` (a limit out of range, an unknown edit scope, an empty forbidden pattern, a negative attempt count, or a `protected_files` pattern that `ValidatePattern` rejects). With invalid rules, `Evaluate` returns only the `invalid_rules` reasons and does not judge the diff.
- **Alternatives:** Trust callers and document the preconditions; skip an invalid `protected_files` pattern when matching (what `IsProtected` still does).
- **Why:** The first version skipped a pattern it could not read, which silently removed that protection: a typo in a protected pattern let the file through. A negative deletion count could offset another file's deletions in the totals. Making the engine reject what it cannot interpret removes the dependence on every caller being correct.
- **Cost if wrong:** A caller that builds `Rules` by hand with a zero value fails with `invalid_rules` instead of getting a default; it should start from `DefaultRules()`.

## D22. How the parser reads git's output

- **Date:** 2026-11-23 (M1, added after review)
- **Decision:** The parser takes the file mode from git's `index <old>..<new> <mode>` line as well as from the `new file mode`, `deleted file mode`, `old mode` and `new mode` lines, so re-pointing an existing symlink or submodule (which has only an `index` line) is reported as `special_file`. It rejects sections git never emits (no mode line, binary marker or hunk; file headers without a hunk), checks each hunk for consistency (line counts against the header, hunks in order and not overlapping, hunks matching the file's added or deleted kind), and drops the one tab git appends to a `---` or `+++` name that contains a space. A change of type, such as a regular file replaced by a symlink, appears in git's output as two sections for one path and fails to parse; so does a path containing a backslash, which the engine cannot match reliably. The code that runs git (`internal/vcs`, M3) **must pin** `diff.suppressBlankEmpty`, `diff.noprefix` and the source and destination prefixes (`a/`, `b/`), because a user's git configuration changes the output format and the parser rejects the changed format.
- **Alternatives:** Read the mode only from the explicit mode lines; accept sections it does not understand and skip them.
- **Why:** Re-pointing an existing symlink produces a patch with no mode line other than `index ... 120000`, so without reading the index mode it would look like an ordinary one-line text edit. Skipping an unrecognized section would let a change hide in it.
- **Cost if wrong:** A type change or a backslash in a path is a parse error, so the fix is rejected even if it was harmless. A parse error must be treated as a rejection by the caller. If M3 forgets to pin the git settings above, `regreen` fails closed on machines that set them.

## D23. Config strings must be real YAML strings; no anchors, aliases or merge keys

- **Date:** 2026-11-23 (M1, added after review)
- **Decision:** Every string-valued key (`name`, `run`, `fix`, `edit_scope`, each `protected_files` and `forbidden_patterns` entry, and the user config's `agent` and `watch.delivery`) must be a YAML string. A plain `true`, `123` or `1.5` is rejected with "must be a string (quote the value)"; the YAML library would otherwise turn it into the text `true`, so `run: true` would run the command `true`. A null list entry is rejected, because the library would otherwise drop it silently. Anchors, aliases and merge keys (`&x`, `*x`, `<<:`) are rejected anywhere in the file. `fix` and the optional keys may be null or omitted.
- **Alternatives:** Accept whatever the library converts; allow anchors and aliases.
- **Why:** A config file that reads one way to a person and another way to the parser is a security problem in a tool that enforces rules, and these conversions are exactly that. Aliases let a short file expand into a very large structure, and let one value appear to say two things.
- **Cost if wrong:** Valid YAML that other tools accept is refused, and people must quote values such as `run: "true"`. The error message says how to fix it.

## D24. Empty path segments are invalid in patterns

- **Date:** 2026-11-23 (M1, added after review)
- **Decision:** `ValidatePattern` rejects a pattern with an empty segment such as `a//b` or `//a`. A single leading `/` (anchor) and a single trailing `/` (directory) are allowed.
- **Alternatives:** Treat `a//b` as `a/b`.
- **Why:** A pattern with a doubled slash is almost always a typo, and quietly guessing what it means could leave a path unprotected. An error costs the user one edit.
- **Cost if wrong:** A pattern some tool generated with a doubled slash must be corrected by hand.
