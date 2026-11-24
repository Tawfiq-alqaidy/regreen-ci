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
- **Updated by D27:** adding a `re:` prefix later would need a migration, not come for free: an existing literal pattern that already starts with `re:` would change meaning.

## D15. `protected_files` use gitignore-style patterns and an in-repo matcher

- **Date:** 2026-11-23 (M1)
- **Decision:** `protected_files` entries are gitignore-style patterns (syntax in `docs/CONFIG.md`), matched by a short matcher in `internal/policy/glob.go` built on `path.Match` from the standard library, with no dependency. A pattern that matches a directory covers everything under it. A leading `!` (negation) is rejected, so configuration can only add protection, never carve out an exception. Malformed patterns are errors.
- **Alternatives:** A third-party gitignore library; plain `path.Match` globs (no `**`, no directory coverage); plain prefixes.
- **Why:** Chosen by the maintainer. People already know gitignore patterns. Writing the matcher in this package keeps the engine free of dependencies and lets the same code validate patterns (`ValidatePattern`) and match them (`MatchPath`), so the two cannot disagree. Rejecting negation closes a way to weaken the always-protected list.
- **Cost if wrong:** The matcher supports a subset of gitignore. Patterns gitignore accepts but this does not (such as a backslash escape) are rejected with an error, not silently misread.
- **Updated by D25 and D27:** the matcher now runs in quadratic time (D25), and patterns with leading or trailing whitespace or a `[!` class are rejected (D27).

## D16. The always-protected list

- **Date:** 2026-11-23 (M1)
- **Decision:** These patterns are hardcoded in `internal/policy/protected.go`: `regreen.yml`, `regreen.yaml`, `.regreen`, `.github`, `.git`, `.githooks`, `.husky`, `.pre-commit-config.yaml`, `lefthook.yml`, `.lefthook.yml`. Each has no slash, so it matches at any depth (`pkg/regreen.yml` is protected), and each covers everything under it when it names a directory. They match case-insensitively, so `.GitHub/` and `Regreen.yml` are protected too. `Evaluate` applies them on every call; no `Rules` value, config key or option removes them. The brief names "git hooks" without listing them: `.githooks`, `.husky`, `.pre-commit-config.yaml`, `lefthook.yml` and `.lefthook.yml` are this project's reading of that phrase (the common places where hook scripts or hook-manager configuration live), and the list can be extended in a later milestone.
- **Alternatives:** Only the three paths the brief names; make the list configurable.
- **Why:** A configurable safety list could be edited away by the same agent it constrains. Case-insensitive matching covers case-insensitive file systems (macOS and Windows by default), where `.GitHub/workflows/ci.yml` is the same file as `.github/workflows/ci.yml`.
- **Cost if wrong:** A legitimate fix that must change one of these files is always rejected, and a human must make it. Hook tools not on the list (for example a different hook manager) are not protected until they are added.
- **Updated by D26:** `.gitmodules` is added to the list. Also, `Evaluate` applies the list whenever it judges a diff, not literally on every call: with invalid rules it rejects the diff before checking any path.

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
- **Updated by D28:** a new file with mode 100755 is also rejected as `special_file`.

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
- **Updated by D25 and D27:** `invalid_path` also covers control characters, invalid UTF-8 and the path limits (D25); `invalid_rules` also covers a forbidden pattern containing a newline (D27); `invalid_diff` also covers an unknown `Special` value (D25).

## D22. How the parser reads git's output

- **Date:** 2026-11-23 (M1, added after review)
- **Decision:** The parser takes the file mode from git's `index <old>..<new> <mode>` line as well as from the `new file mode`, `deleted file mode`, `old mode` and `new mode` lines, so re-pointing an existing symlink or submodule (which has only an `index` line) is reported as `special_file`. It rejects sections git never emits (no mode line, binary marker or hunk; file headers without a hunk), checks each hunk for consistency (line counts against the header, hunks in order and not overlapping, hunks matching the file's added or deleted kind), and drops the one tab git appends to a `---` or `+++` name that contains a space. A change of type, such as a regular file replaced by a symlink, appears in git's output as two sections for one path and fails to parse; so does a path containing a backslash, which the engine cannot match reliably. The code that runs git (`internal/vcs`, M3) **must pin** `diff.suppressBlankEmpty`, `diff.noprefix` and the source and destination prefixes (`a/`, `b/`), because a user's git configuration changes the output format and the parser rejects the changed format.
- **Alternatives:** Read the mode only from the explicit mode lines; accept sections it does not understand and skip them.
- **Why:** Re-pointing an existing symlink produces a patch with no mode line other than `index ... 120000`, so without reading the index mode it would look like an ordinary one-line text edit. Skipping an unrecognized section would let a change hide in it.
- **Cost if wrong:** A type change or a backslash in a path is a parse error, so the fix is rejected even if it was harmless. A parse error must be treated as a rejection by the caller. If M3 forgets to pin the git settings above, `regreen` fails closed on machines that set them.
- **Updated by D28 and D29:** mode lines accept only four modes and backslash lines only the exact marker (D28); the git command gains `--ignore-submodules=none`, and some settings omit changes rather than change the format (D29).

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

## D25. A quadratic path matcher, path limits and quoted messages

- **Date:** 2026-11-24 (M1, final review)
- **Decision:** `MatchPath` matches by dynamic programming over (pattern segments) x (path segments), collapsing runs of `**` and answering the "whole path or any leading directory" rule from the table's last row instead of looping over prefixes. One function, `checkRepoPath`, checks paths for both the parser and `Evaluate`; besides the earlier rules it rejects bytes below 0x20 and DEL, invalid UTF-8, paths over 4096 bytes and paths with more than 255 segments. Every path and pattern in a `Reason.Message` is printed with `%q`, and a `FileChange.Special` value the parser never produces is `invalid_diff`.
- **Alternatives:** Keep the recursive matcher and cap the number of `**` segments; strip or escape control characters instead of rejecting them.
- **Why:** The recursive matcher backtracked over each `**` and retried every prefix: one pattern from review took 1.67 s on a path of 100 repeated `test/fixtures/` segments, and eight `**` against 40 segments did not finish. The path is chosen by the agent, so that was a way to stall the check. A file name with a newline or an ESC sequence was printed raw and could forge CI log lines. Two copies of the path check had already drifted once. The old matcher is kept in the tests as a reference and compared with the new one on 5000 random pairs.
- **Cost if wrong:** A legitimate path with a control character, non-UTF-8 bytes (for example a Latin-1 file name, which git prints as octal escapes), more than 4096 bytes or more than 255 segments is a parse error, so a fix touching it needs a human.

## D26. `.gitmodules` is always protected

- **Date:** 2026-11-24 (M1, final review)
- **Decision:** `.gitmodules` joins the always-protected list (D16), matched case-insensitively at any depth like the others.
- **Alternatives:** Leave it to `protected_files`; rely on rejecting submodule changes (D18).
- **Why:** `.gitmodules` sets submodule URLs, and `ignore = all` in it makes `git diff` leave submodule changes out entirely, so a diff that edits it can hide a later change from the engine (checked in a throwaway repository). Rejecting submodule changes does not help with a change git never prints. This extends the brief's hardcoded list (section 3 names `regreen.yml`, `.github/**`, `.regreen/**` and git hooks); the maintainer can revisit it.
- **Cost if wrong:** A legitimate fix that must change `.gitmodules` is always rejected and needs a human.

## D27. Patterns that can never match, or match the wrong thing, are rejected

- **Date:** 2026-11-24 (M1, final review)
- **Decision:** `ValidatePattern` rejects a `protected_files` pattern with leading or trailing whitespace, and any pattern containing `[!`, with a message that says to use `[^...]`. `Evaluate` reports a forbidden pattern containing a newline as `invalid_rules` (the config loader already rejected it).
- **Alternatives:** Trim whitespace; translate `[!` to `[^` as gitignore would.
- **Why:** A pattern with edge whitespace validated but could never match a path, so it protected nothing. Go's `path.Match` reads `[!x]` as a class containing `!` and `x`, unlike gitignore, so `[!a]*.go` would protect the wrong files without any error. Silently translating would mean two syntaxes for one thing; an error costs one edit. A forbidden pattern with a newline can never match an added line, which never contains one.
- **Cost if wrong:** A pattern copied from a `.gitignore` that uses `[!...]` must be rewritten with `[^...]`.

## D28. New executable files are rejected; only known modes and markers parse

- **Date:** 2026-11-24 (M1, final review)
- **Decision:** A new file with mode 100755 gets `Special = "executable"` and is rejected as `special_file`. Modifying or deleting an existing executable file (100755 on the `index` or `deleted file mode` line) stays allowed and is not special. Mode and `index` lines accept only 100644, 100755, 120000 and 160000. Inside a hunk, a backslash line is accepted only if it is exactly `\ No newline at end of file`. A hunk that removes lines must start at old line 1 or later.
- **Alternatives:** Accept any six-digit octal mode; allow new executables and let the checks catch problems.
- **Why:** Before this, `new file mode 040000` parsed as an ordinary added file. A new executable is a new thing that can be run (a script a hook or a build step could pick up), which D18 already treats as outside a mechanical fix when it arrives as a mode change; a new file should not be a way around that. Editing an existing script's content is an ordinary text change and stays allowed. Unknown backslash lines and impossible hunk positions are not something git emits.
- **Cost if wrong:** A fix that has to add an executable script needs a human.

## D29. The git command includes `--ignore-submodules=none`

- **Date:** 2026-11-24 (M1, final review)
- **Decision:** The documented command is `git -c core.quotepath=off diff --no-color --no-ext-diff --no-textconv --no-renames --ignore-submodules=none <base> <head>`, and `docs/ARCHITECTURE.md` carries a git invocation checklist for M3 and M4: run from the root with `--no-relative`, pin the format settings with `-c` and the prefixes with `--src-prefix=a/ --dst-prefix=b/`, treat `.git/config` and `.gitmodules` as hostile, cap the output size, and consider `git diff-tree -r -p`.
- **Alternatives:** Pin only the settings that change the output format (D22).
- **Why:** D22 assumed a wrong git setting would change the format, which the parser rejects. Some settings instead make git omit changes: `submodule.<name>.ignore`, `diff.ignoreSubmodules` and `.gitmodules` `ignore = all` hide submodule pointer changes, and `diff.relative` hides changes outside the current directory. The parser cannot detect a change git never printed, so the caller must override these with command-line flags. The agent can write `.git/config` in its checkout without it ever appearing in a diff. The brief asked to pin the prefixes with `-c`; git 2.39.5, on which the checklist was checked, documents no config key for them, so the checklist uses the command-line flags.
- **Cost if wrong:** If M3 misses a setting that omits changes, the engine judges an incomplete diff and fails open for that change. The checklist is the guard until M3 has tests that run real git with hostile configuration.

## D30. For M2: `ErrorFiles` is the one input that widens what the agent may touch

- **Date:** 2026-11-24 (M1, final review; a constraint on M2, nothing built)
- **Decision:** M2 should derive `ErrorFiles` from the original failure (the CI run on the commit before any regreen attempt), or intersect it with the files that existed at the base commit.
- **Alternatives:** Extract `ErrorFiles` from the latest CI log on every attempt.
- **Why:** Under `edit_scope: files_in_errors`, `ErrorFiles` is the only input that adds files the agent may edit; the rules (read from the base branch in M4) and the diff cannot. From the second attempt on, the CI log comes from a run of the agent's own pushed commit, so the agent can influence which files the log names (for example by adding a file whose name, or whose errors, then appear in the log) and so widen its own scope.
- **Cost if wrong:** Taking the names only from the first failure means a second attempt cannot fix an error the first attempt revealed in another file; that needs a human or a new run.

## D31. go-github v93.0.0 is the GitHub client

- **Date:** 2026-11-24 (M2)
- **Decision:** `internal/ci/github` uses `github.com/google/go-github/v93` at exactly v93.0.0 for the Actions API (list runs, list jobs, get a job, ask for a job log location). It is the only new direct dependency. It brings one indirect dependency, `github.com/google/go-querystring` v1.2.0, which go-github uses to build query strings. The client is built with `github.NewClient(opts...)` and the options `WithAuthToken`, `WithUserAgent`, `WithTimeout`, and, for tests only, `WithURLs` and `WithHTTPClient`.
- **Alternatives:** Call the REST API with `net/http` and decode JSON into our own structs.
- **Why:** The library already has the request paths, the query parameters, the response types and the pagination fields, and the maintainers of GitHub's own client keep them current. Raw `net/http` would mean writing and testing those ourselves, and a typo in a path or a field name would only show up against the real API. The library also refuses to follow a redirect to another host while the token is attached (`checkRedirectHost` in its source), and the adapter downloads the log with a separate client that has no token (D37; a test checks that the download request carries no `Authorization` header). The cost is the dependency, and a signature change when the major version moves; the version is pinned in `go.mod`, and the code was written against signatures read with `go doc`, not from memory.
- **Cost if wrong:** If the library is abandoned or changes shape, the adapter is one package (`internal/ci/github`) and the rest of regreen sees only `ci.Provider`, so replacing the client touches that package alone.

## D32. The provider interface is `ci.Provider`

- **Date:** 2026-11-24 (M2)
- **Decision:** The interface the brief calls `CIProvider` is named `ci.Provider`. It has three methods: `Name() string`, `CommitStatus(ctx, repo, commit) (CommitStatus, error)` and `FailureLog(ctx, repo, id) (StepLog, error)` (the return type changed in the final review, D42). The package `internal/ci` holds only provider-neutral types and does no I/O; a test (`TestCIPackageIsPure`) keeps `os`, `os/exec`, `net` and `net/http` out of it.
- **Alternatives:** Keep the brief's name `ci.CIProvider`.
- **Why:** Go style avoids repeating the package name in a type name (`ci.CIProvider` reads "ci CI provider"). The role is the same. `CommitStatus` reports the aggregate state and the failed steps; `FailureLog` turns one failed step's ID into that step, its commit and its raw log text, so the caller needs nothing but the ID from the first call to make the second.
- **Cost if wrong:** A rename. Nothing outside the repository refers to the name yet.

## D33. Failure IDs are stateless to resolve, but untrusted: `gha-<run>-<job>-<step>`

- **Date:** 2026-11-24 (M2; changed in the final review)
- **Decision:** A failed step's ID is `gha-<runID>-<jobID>-<stepNumber>`, for example `gha-100-200-3`. The step number is 0 when the job failed without a failed step, and the job ID is 0 when the run failed without any failed job. `parseID` refuses input longer than 64 bytes before anything else (its errors quote at most 64 bytes of the input) and accepts only that exact shape: decimal numbers of 1 to 19 digits, a run ID above 0, a job ID and step number that fit their types, so every ID `encodeID` builds from non-negative numbers parses back to the same numbers. An ID is caller-supplied (in M3 the agent passes it to the tools), so it is never trusted. `FailureLog` reads the job from GitHub and rebuilds the step and the commit from that record (`ci.StepLog`, D42); the ID it returns is the canonical re-encoding, so `gha-0100-200-3` comes back as `gha-100-200-3`. It refuses, before downloading anything, an ID whose job belongs to another run; whose job is not completed or passed (`success`, `skipped`, `neutral`); whose step number is not a step of the job with conclusion `failure`; or whose step number is 0 although the job has a failed step (a step-0 ID is valid only for a failed job with no failed step, which is exactly when `CommitStatus` issues one). It also refuses a job without a full head commit. A run-level ID (job 0) has no log and no commit binding: `FailureLog` returns only its canonical ID, without any request. Callers must compare `StepLog.Commit` with the commit they are working on and reject a mismatch.
- **Alternatives:** Random IDs with a lookup table in a state file; an opaque encoded string; trusting the ID's step number and returning the whole job log when the step is not found (the first version).
- **Why:** The same failure gets the same ID on every call and in every process, so M3's `get_failure` needs no state to resolve an ID, and a test can write the ID down by hand. Nothing secret is in it. The cost of being readable is that a caller can construct an ID: one for a job on another commit, for a passing step, or for a step the job does not have. The log `FailureLog` returns feeds `Failure.Files`, which widens what the agent may edit, so such an ID must not yield a log: hence the checks above, and the commit in the result so that the caller can see which commit the job ran for.
- **Cost if wrong:** If another provider needs a different shape, it picks its own prefix; callers treat the ID as opaque (`ci.FailedStep.ID` says so). Resolving an ID costs one API call for the job before the log download. A run-level failure cannot be bound to a commit through `FailureLog`; M3 gets its commit from `CommitStatus`.

## D34. Which runs count, and when the status is final

- **Date:** 2026-11-24 (M2; changed during review)
- **Decision:** `CommitStatus` lists the workflow runs whose `head_sha` equals the commit, whatever the event (`push`, `pull_request`, and so on). The status is `pending` if any of those runs is not `completed`, including a run that a newer run has replaced. If every run is completed, runs are grouped by (workflow, event, head branch) and only the newest of each group counts, by run number and then attempt, so a re-run replaces the run it re-ran and a newer push replaces an older one. No runs at all is `no_runs`. A listing that GitHub reports as larger than what arrived (`total_count` above the number received), or more than 10 pages of 100, is an error, not a guess. The same holds for the job list of a run.
- **Alternatives:** Count only the newest run per workflow and event (the plan's first version, which also decided pending from the kept runs only); count only `push` runs; decide from the first page.
- **Why:** The maintainer's rule is that status stays `pending` until every run for the commit has completed, so pending is decided over all of them. The branch is part of the group key because one commit can be the head of two branches (a push and a pull request from another branch); a passing run on one must not hide a failing run on the other. Partial data is refused because a missing page could be the failing run.
- **Cost if wrong:** A stale run that nothing replaces (say, on a branch nobody pushes to again) keeps the commit `pending` or `failure` until it is re-run or deleted. A commit with more than 1000 runs cannot be judged and returns an error.

## D35. Conclusions are mapped fail closed

- **Date:** 2026-11-24 (M2)
- **Decision:** A completed run or job passes only if its conclusion is `success`, `skipped` or `neutral`. Every other completed value (`failure`, `timed_out`, `cancelled`, `startup_failure`, `action_required`, `stale`, and any value GitHub adds later) counts as failing. For a failing run, each completed non-passing job yields one `ci.FailedStep` per step whose conclusion is `failure`. A failing job with no such step (cancelled, timed out, runner lost) yields one entry with an empty step name and step number 0, and a failing run with no failing job yields one entry with an empty job name. So a `failure` status always carries at least one failed step. Failed steps are listed run by run in ascending run ID order.
- **Alternatives:** List the known failing values and treat the rest as passing; skip jobs with no failed step.
- **Why:** An unknown value that counts as passing could report a broken commit as green. A cancelled job with no step to name is still a reason the commit is not green, so it still gets an entry the caller can show.
- **Cost if wrong:** A value GitHub adds that is harmless is reported as a failure until the list in `passes` learns it. A `cancelled` run, which a person may have cancelled on purpose, is reported as failing.

## D36. A step's log is cut out of the job log by timestamp window

- **Date:** 2026-11-24 (M2; changed in the final review)
- **Decision:** GitHub gives one log per job, not per step. `FailureLog` takes the failed step's start and end time from the job's step list and keeps the log lines whose leading RFC 3339 timestamp is at or after the start and before the end plus one second. The API reports step times truncated to the second, so the extra second keeps the step's last lines. A line with no timestamp belongs with the line before it. If the step's times are unusable (missing, or the end before the start) or the window selects no line, `FailureLog` returns an empty log marked `Partial` (D42). A step-0 ID (a failed job without a failed step) gets the whole job log, which is the best text there is for it. Then GitHub's per-line timestamps and its workflow markers are stripped (`##[group]`, `##[command]`, `[command]` and `##[debug]` removed; `##[error]`, `##[warning]` and `##[notice]` turned into `error: `, `warning: ` and `notice: `; `##[endgroup]` lines dropped). Windows line endings are handled: a trailing `\r` counts as part of the line ending when a timestamp is parsed and when a line is compared with `##[endgroup]`, and it is kept on the line (`TrimLog` later turns `\r\n` into `\n`).
- **Alternatives:** Download the run-logs zip archive, which has one file per step, and read the step's file. Return the whole job log when the window selects nothing (the first version).
- **Why:** The archive is a zip from an untrusted source: reading it means a zip parser, limits on entry count and expanded size, and file names that come from the log host. The job log is plain text we can cap while we read it. Its cost is precision. The whole job log is not a fallback for a numbered step, because the other steps' output would then feed `Failure.Files` (D40) and widen what the agent may edit; an empty, `Partial` log says honestly that the step's text could not be isolated.
- **Cost if wrong:** Steps that run within the same second can bleed into each other, so an excerpt can include a neighbouring fast step's lines. A composite action or a reusable workflow is one step to GitHub, so a failure inside it is reported as the calling step. A log without timestamps, or a step with no usable times, yields no text at all for a numbered step. A byte-order mark at the start of a log is removed (fixtures cover it).

## D37. The log is downloaded without the token, and only its tail is kept

- **Date:** 2026-11-24 (M2; changed in the final review)
- **Decision:** `FailureLog` asks the API for the job's log location (a signed URL), then downloads it with a separate `http.Client` that has no credentials. The location must be an absolute URL with a non-empty host; a relative or host-less location is refused (`log download URL is missing`) and never resolved against the API base. It must be `https`; `http` is accepted only when a test has set an `http` API base on a loopback host (`localhost`, `127.0.0.0/8`, `::1`) and the download host is loopback too, and `New` refuses an `http` base on any other host. The same check runs on each redirect, with at most two redirects. At most 8 MiB is kept, and when the log is bigger the end is kept, starting at a line boundary, because a step reports its failure at the end; such a log is marked `Partial` (D42). The reader never holds much more than twice the cap. Errors name the job, the repository and at most an HTTP status or one of these fixed causes: the context's error (`context.Canceled` or `context.DeadlineExceeded`); for the location request, `HTTP status N`, `network error`, `network error: timeout` or `request failed`; for the location itself, `log download URL is missing` or `log download URL scheme is not allowed`; for the download and the read of its body, `too many redirects`, the two location causes (from a redirect), `download timed out`, `download failed` or `HTTP status N`. No error from go-github for the location request, from `http.Client.Do` or from the body read is wrapped, since any of them can carry the URL.
- **Alternatives:** Download through the API client; read the whole body; keep the head of a large log; resolve a relative location against the API base (the first version, which would fetch an API page as the log).
- **Why:** The signed URL is a secret that grants access to the log, and the log host is not the API host, so the API token has no business there. Bounding the read means a huge or endless response cannot use all memory. A plain-`http` base anywhere but loopback would send the token in clear text.
- **Cost if wrong:** A failure early in a log larger than 8 MiB is not seen. There are no retries and no rate-limit handling: a 403 or 429 is an error with its status. The fixed causes say little; a person debugging a failed download sees `download failed` and the job number, not the transport's message.

## D38. Token lookup: `GH_TOKEN`, then `GITHUB_TOKEN`, then `gh`; github.com only

- **Date:** 2026-11-24 (M2)
- **Decision:** `Token(ctx)` returns the first of: the `GH_TOKEN` variable, the `GITHUB_TOKEN` variable, the output of `gh auth token --hostname github.com` (run with a 10 second limit, fixed arguments, and the binary found by `exec.LookPath`). A value with whitespace or control characters in it is an error. If nothing is found the error is `ErrNoToken`. A cancelled or timed-out lookup returns the context error, not `ErrNoToken`. The token is never logged or written to disk. `Provider` formatted with `%v`, `%+v` or `%s` prints the fixed string `github-actions provider`; `%#v` prints the struct in Go syntax, which shows the go-github client only as a pointer address. None of them contains the token (`TestProviderDoesNotPrintToken`). Only the host `github.com` is accepted; any other host in `ci.Repo` is an error.
- **Alternatives:** Read only the environment; support GitHub Enterprise hosts now.
- **Why:** The two variables are what GitHub's own tools use, and `gh` covers a developer who is logged in but exports nothing. Enterprise hosts need their own API base URL and their own token lookup, and none of that has been asked for; refusing the host clearly is better than sending a github.com token to another host.
- **Cost if wrong:** GitHub Enterprise users cannot use regreen until a later milestone adds host-specific URLs and `gh auth token --hostname <host>`.

## D39. A failed step is mapped to a check by name or command

- **Date:** 2026-11-24 (M2; maintainer decision 1)
- **Decision:** `MatchCheck` maps a failed step to a `regreen.yml` check without any config change. It tries three levels and the first that finds exactly one check wins. (1) The step's command equals a check's `run`, or the step's name equals a check's `name` (ignoring case). The step's command is its name without a leading `Run ` (GitHub's default name for a `run:` step). (2) The command contains a check's `run` at word boundaries; if several checks match, the longest `run` wins. (3) The job name equals a check's name. Whitespace is collapsed before comparing. If a level finds several different checks, the result is ambiguous and the step is unmatched, with no fallback to a later level. An unmatched step is `Fixable == false`.
- **Alternatives:** Add a `workflow`/`job` key to each check in `regreen.yml`; match by substring anywhere.
- **Why:** The maintainer chose to match on what is already in the config. Word boundaries stop `npm test` from matching `npm testing`. Failing closed on ambiguity means regreen never picks a fix command by a coin toss.
- **Cost if wrong:** A step whose name has nothing in common with its check is not fixable by regreen; the user renames the step or the check. Level 3 can match an unrelated step in a job that is named like a check.

## D40. Error files are the known files the log names (resolves D30 in part)

- **Date:** 2026-11-24 (M2; maintainer decision 2; extended in the final review)
- **Decision:** `ExtractFiles(text, known, maxFiles)` returns the entries of the caller's `known` set that the trimmed log text names, sorted, unique, at most 100 (the default). The file system is never read. It runs two passes.
  - **Tokens.** The text is split on whitespace and on `" ' ` ( ) [ ] { } < > , ; = |`. A trailing `:12`, `:12:5`, `:` or `.` and a leading `./` are removed, and backslashes become `/`. A token matches if it equals a known path, or if it is an absolute path (a leading `/` or a drive letter) that ends with `/` and a known path. Any token with a `..` segment is ignored. A token over 4096 bytes is ignored. The last token of a line that carries the cut marker is ignored too, since it may be half of a longer name.
  - **Names with whitespace or delimiters.** No token can hold a known path such as `pages/[id].tsx`, `app/(auth)/page.tsx`, `src/a=b.ts` or `docs/my file.md`, so a second pass searches each line (backslashes read as `/`, the cut marker removed) for each known path that contains whitespace or a delimiter character. An occurrence counts when its right side is the end of the line, whitespace, a quote (`"`, `'`, `` ` ``) or one of `: ( , ; )`, but not the point where a line was cut; and its left side is the start of the line, whitespace, a quote or one of `( [ < =`, or a `/` where the part of the surrounding token (split as in the first pass) before the occurrence is an absolute path (`/...` or `X:/...`) or only `./` prefixes, without a `..` segment. So `x/pages/[id].tsx` does not name `pages/[id].tsx`, and `pages/[id].tsx.bak` names only `pages/[id].tsx.bak`. Each such path costs one linear scan of the text (a Knuth-Morris-Pratt search). When `known` holds more than 2000 such paths the second pass is skipped.
  `failure.Build` extracts files from the trimmed excerpt, not from the raw log, so a path that only appears in a part that was cut does not count.
- **Alternatives:** Treat every path-like token as a file (even one not in the repository); read the file system to check existence; leave names with delimiters out (the first version).
- **Why:** `ErrorFiles` is the one input that can widen what the agent may edit (D30). Taking only names the caller already knows keeps a log from naming a new path, and a pure function with a set argument is easy to test. Names with brackets and parentheses are normal in web frameworks (dynamic routes, route groups), and a file the errors name but `files_in_errors` cannot allow makes regreen useless for that failure. The left-hand rule for a `/` is the first pass's suffix rule for absolute paths, so a relative prefix still never matches.
- **Cost if wrong:** A repository with more than 2000 known paths containing whitespace or delimiters gets none of them from the second pass. A name followed by a character outside the right-boundary set (`.`, `]`, `>`) is not found by the second pass. **What M3 must still do for D30:** the set `known` must come from git at the original failing commit (or the base), never from the agent's later push, and `Build` must be called on the log of the original failure for the files, not on a later run's log. M2 gives M3 the function and the parameter; it cannot enforce where M3 gets the set. Even with a correct set, a log can name any known file, so this bounds the widening to existing files of the base; it does not make log text trustworthy.

## D41. What `TrimLog` does to untrusted log text

- **Date:** 2026-11-24 (M2; extended in review)
- **Decision:** `TrimLog` is applied to every excerpt. In order: (1) invalid UTF-8 becomes U+FFFD; (2) `\r\n` and `\r` become `\n`, and U+2028, U+2029 and U+0085 (NEL) become `\n` too, so a Unicode line separator cannot forge a line break; (3) ANSI escape sequences are removed (CSI, `ESC [` up to its final byte, and OSC, `ESC ]` up to BEL or `ESC \`; one not ended on its line stops at the newline, so it cannot swallow the rest of the log), then all other control characters except newline and tab, and every Unicode format character (category Cf: bidirectional overrides and isolates, zero-width characters, tags, soft hyphen); (4) trailing blanks are trimmed, three or more blank lines collapse to two, and leading and trailing blank lines are dropped; (5) a line longer than `MaxLineBytes` is cut at a rune boundary and ends with ` …[line cut]`; (6) the last lines that fit `MaxLines` and `MaxBytes` are kept, and if earlier lines were dropped, blank lines at the start of what is kept are dropped too and the text starts with `[… N earlier lines trimmed …]`, N counting all of them. Defaults: 200 lines, 16384 bytes, 2000 bytes per line. A limit that is not positive takes its default. Every cut inside a line carries the marker: `MaxLineBytes` is lowered to `MaxBytes` minus the marker if needed, and `MaxBytes` has a floor of the marker plus one rune (18 bytes), so there is no unmarked cut.
- **Alternatives:** Only strip ANSI; escape instead of delete; keep format characters.
- **Why:** The excerpt is shown to a person and to an agent. Bidirectional overrides make text read differently from how it is stored, zero-width and tag characters hide text, and a fake line separator forges structure; all of those are ways to make a log say something else to the reader. Deleting them is simpler to reason about than escaping.
- **Cost if wrong:** Emoji sequences joined by a zero-width joiner and soft hyphens lose a character. A `MaxBytes` below 18 is not honoured exactly. The header line is outside the caps, so the excerpt can be longer than `MaxBytes` by that line and hold `MaxLines + 1` lines. The kept tail (the excerpt without its header line) is a fixpoint: trimming it again with the same limits returns it unchanged (`FuzzTrimLog` checks this). None of this makes the text trustworthy: a log can still say anything in plain words, so the excerpt must be shown as data (see the trust model in `ARCHITECTURE.md`).

## D42. `FailureLog` returns a `ci.StepLog`; `Partial` makes `Failure.Truncated`

- **Date:** 2026-11-24 (M2, final review)
- **Decision:** `Provider.FailureLog` returns `ci.StepLog{Step, Commit, Log, Partial}` instead of a string. `Step` is a `ci.FailedStep` rebuilt from the provider's own records (for GitHub: the canonical ID, the job's workflow name, the job name, the step name and number, and the job's web URL), `Commit` is the full object name the job ran for, `Log` is the step's raw text with the provider's framing removed (untrusted), and `Partial` is true when `Log` is known to be incomplete or not limited to the step: the GitHub adapter sets it when the 8 MiB tail cut dropped the start of the log, and when a numbered step's time window is unusable or selects no line (then `Log` is empty, D36). `failure.Build(sl, checks, known, lim)` takes the `StepLog`, copies the step and the commit from it, and sets `Failure.Truncated` when `TrimLog` cut anything or `sl.Partial` is set. Callers must reject a `StepLog` whose `Commit` is not the commit they are fixing. A run-level ID yields a `StepLog` with only `Step.ID` set: no log, no commit, not `Partial`.
- **Alternatives:** Keep `(string, error)` and let the caller pass the step and the commit to `Build` (the first version); return the job and step IDs only.
- **Why:** The ID comes from the agent (D33). With a string result, M3 would have had to trust its own copy of the step for an ID it cannot verify, and could not tell that an ID named a job on another commit. With the step and commit rebuilt by the provider, M3 can check the binding, and `Build` describes the step the log really came from. `Partial` keeps "this is not the whole story" from being lost between the adapter and the excerpt: an agent or a person reading `Truncated` knows to treat a short or empty excerpt with care.
- **Cost if wrong:** An interface change for any future provider, which must also rebuild the step and commit. `Truncated` no longer means only "TrimLog cut something", so a caller that wants that alone must call `TrimLog` itself.
