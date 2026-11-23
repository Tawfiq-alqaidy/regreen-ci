# Configuration reference

This describes the two configuration files regreen reads and the rules the policy engine applies.

**Status (M1):** the code that loads and validates these files, and the policy engine that applies them, exist and are tested (`internal/config`, `internal/policy`). No command reads these files yet. `regreen verify`, `regreen init` and the MCP server arrive in M3 and M4. Until then the only way to run this code is through the Go tests; see [the M1 summary](milestones/M1.md).

**Limit:** the policy checks that a fix follows the rules. It cannot tell whether a fix is correct. A forbidden pattern is a literal substring, so an agent can often reword around it (see "forbidden_patterns" below). Human review before merge is part of the design.

## Repo config: `regreen.yml`

The file sits at the repository root. It must be one YAML document, at most 1 MiB. Unknown keys are errors, so a misspelled key cannot silently turn a protection off. All problems are reported together, each starting with the file name.

A complete example:

```yaml
checks:
  - name: lint
    run: npm run lint
    fix: npx eslint --fix .
  - name: test
    run: npm test
policy:
  protected_files: [".eslintrc*", "tsconfig.json"]
  forbidden_patterns: ["eslint-disable", "@ts-ignore", "@ts-expect-error"]
  max_changed_lines: 200
  max_deleted_lines: 50
  max_attempts: 2
  edit_scope: files_in_errors
```

A minimal one is just the checks; every `policy` key then takes its default:

```yaml
checks:
  - name: lint
    run: make lint
```

### Types

Every key that holds text must be a real YAML string. An unquoted `true`, `123` or `1.5` is rejected with `must be a string (quote the value)`; write `run: "true"` if you mean the command `true`. Anchors, aliases and merge keys (`&name`, `*name`, `<<:`) are rejected anywhere in the file.

### `checks` (required)

A list with at least one entry. Each entry has:

| Key | Type | Required | Rule |
|---|---|---|---|
| `name` | string | yes | Starts with a letter or digit; then only letters, digits, `.`, `_` and `-`. Unique within the file. |
| `run` | string | yes | Not empty or blank; no NUL byte. The command that performs the check. |
| `fix` | string | no | If present, not empty or blank; no NUL byte. A deterministic fix command. A null value counts as absent. |

The commands are stored as written. The config package never runs or interprets them; running them is for a later milestone.

### `policy` (optional)

The whole section may be omitted. Each key may be omitted; a null value also counts as omitted.

| Key | Type | Default | Rule |
|---|---|---|---|
| `protected_files` | list of strings | none | Extra protected paths, as patterns (below). Each must be a valid pattern. |
| `forbidden_patterns` | list of strings | none | Literal text a fix may not add (below). No entry may be empty or contain a newline. A null entry is rejected. |
| `max_changed_lines` | integer | `200` | From 1 to 1,000,000. Limit on lines added plus lines deleted. |
| `max_deleted_lines` | integer | `50` | From 0 to 1,000,000. Limit on lines deleted. |
| `max_attempts` | integer | `2` | From 1 to 1,000,000. How many fix attempts are allowed. |
| `edit_scope` | string | `files_in_errors` | `files_in_errors` or `any`. |

The defaults are deliberately strict: a config that says nothing is still a safe config.

### `edit_scope`

- `files_in_errors`: a fix may change only files named in the CI failure's errors. `Evaluate` receives that list from its caller (as `Input.ErrorFiles`); it trusts the list. The code that extracts file names from CI logs is M2.
- `any`: a fix may change any file that is not protected.

With `files_in_errors` and no files named, every file is out of scope.

### `forbidden_patterns`

Each entry is literal text, matched case-sensitively as a substring of a line the diff adds. Lines the diff removes or leaves alone are never checked, so removing an existing `eslint-disable` comment is always allowed. A match is reported with the file and the line number in the new version of the file. At most 20 matches are listed, followed by one reason that says how many more there were.

Literal matching is easy to explain and cannot be written wrongly in a way that matches nothing, but it is also easy to evade: `eslint-disable` does not catch a different suppression syntax for the same linter, or a rewording that avoids the exact text. Choose patterns for the habits you actually see, and keep reviewing diffs.

### `protected_files` pattern syntax

Patterns are gitignore-style, matched against repo-relative paths with `/` separators. They are case-sensitive.

- A pattern with no slash (other than a trailing one) matches a path segment at any depth: `tsconfig.json` matches `tsconfig.json` and `packages/app/tsconfig.json`.
- A pattern with a slash in the middle is anchored at the repository root: `docs/api` matches `docs/api/v1/x.md` but not `x/docs/api`.
- A leading `/` anchors the pattern at the root: `/tsconfig.json` matches only the root file.
- A trailing `/` is allowed and ignored; the pattern then names a directory.
- A pattern that matches a directory covers everything under it: `node_modules` matches `a/node_modules/x/y.js`.
- `*` matches any run of characters within one path segment, `?` one character, and `[abc]` or `[a-z]` one character from a set. These are the rules of Go's `path.Match`.
- `**` as a whole segment matches zero or more segments: `src/**/gen/*.go` matches `src/gen/a.go` and `src/a/b/gen/a.go`, but not `src/a/b/gen/sub/a.go`. `**` mixed with other characters in a segment (`a**b`) is an error.

These examples come straight from the `TestMatchPath` table in `internal/policy/glob_test.go`:

| Pattern | Path | Matches |
|---|---|---|
| `tsconfig.json` | `tsconfig.json` | yes |
| `tsconfig.json` | `packages/app/tsconfig.json` | yes |
| `tsconfig.json` | `tsconfig.json.bak` | no |
| `/tsconfig.json` | `tsconfig.json` | yes |
| `/tsconfig.json` | `packages/app/tsconfig.json` | no |
| `.eslintrc*` | `.eslintrc.json` | yes |
| `.eslintrc*` | `web/.eslintrc.cjs` | yes |
| `.eslintrc*` | `eslintrc.json` | no |
| `.github/**` | `.github/workflows/ci.yml` | yes |
| `.github/**` | `.github` | yes |
| `.github/**` | `x/.github/workflows/ci.yml` | no |
| `docs/api` | `docs/api/v1/x.md` | yes |
| `docs/api` | `docs/apiary.md` | no |
| `src/**/gen/*.go` | `src/a/b/gen/a.go` | yes |
| `src/**/gen/*.go` | `src/a/b/gen/sub/a.go` | no |
| `**/secret.txt` | `secret.txt` | yes |
| `*.lock` | `a/b/c.lock` | yes |
| `a/*.go` | `a/b/c.go` | no |
| `Tsconfig.json` | `tsconfig.json` | no |

A pattern is invalid, and `ParseRepo` rejects the file, if it is empty or blank; starts with `!`; contains a backslash, a NUL byte or a newline; is only `/`; has an empty segment (`a//b`); has a segment `.` or `..`; mixes `**` with other characters in a segment; or has a malformed bracket (`a[`).

Negation (`!`) is rejected on purpose. Configuration can add protection but never remove it, so a `!regreen.yml` line cannot undo the always-protected list.

### Always-protected paths

These ten patterns are built into the policy engine. No config key or option turns them off, and `protected_files` can only add to them.

```
regreen.yml
regreen.yaml
.regreen
.github
.git
.githooks
.husky
.pre-commit-config.yaml
lefthook.yml
.lefthook.yml
```

They have no slash, so they match at any depth (`pkg/regreen.yml` is protected) and cover everything under a directory (`.github/workflows/ci.yml`). They match case-insensitively, so `Regreen.yml` and `.GitHub/` are protected on file systems that ignore case. Your `protected_files` patterns stay case-sensitive.

The brief asks for "git hooks" to be protected without listing them. `.githooks`, `.husky`, `.pre-commit-config.yaml`, `lefthook.yml` and `.lefthook.yml` are this project's reading of that phrase (decision D16), and the list can be extended. `.gitignore` is not in the list, and neither is anything else not named above.

### Rejection reasons

`policy.Evaluate` returns a verdict with a list of reasons. Each reason has a `Rule` (the names below, also available as the `Rule*` constants in `internal/policy`), a `File` and `Line` when it concerns one, and a one-sentence message meant for a fixing agent. The diff is allowed exactly when the list is empty.

| Rule | Meaning |
|---|---|
| `invalid_rules` | The rules cannot be interpreted: a limit out of range, an unknown `edit_scope`, an invalid `protected_files` pattern, an empty forbidden pattern, a negative attempt count. Only these reasons are returned; the diff is not judged. Rules from `ParseRepo` never trigger this. |
| `max_attempts` | The attempts already used reached `max_attempts`. The agent should stop and report to the developer. |
| `empty_diff` | The diff changes no files. |
| `invalid_path` | A path in the diff is not a clean repo-relative path (absolute, `..`, backslash, NUL byte, not normalized). |
| `invalid_diff` | A file entry could not have come from git: a negative deletion count, a duplicate path, an unknown change kind, a binary change with line counts, an added line numbered below 1. |
| `protected_file` | The file matches an always-protected pattern or one of `protected_files`. The message says which. |
| `edit_scope` | With `files_in_errors`, the file is not named in the errors. |
| `binary_file` | A binary change. |
| `special_file` | A symlink, submodule or file-mode change. |
| `forbidden_pattern` | An added line contains a forbidden pattern. |
| `max_deleted_lines` | The diff deletes more lines than `max_deleted_lines`. |
| `max_changed_lines` | Lines added plus lines deleted exceed `max_changed_lines`. |

The order in which the engine applies them is in [the architecture document](ARCHITECTURE.md).

### Example errors

These are real messages from `config.ParseRepo`. A value that is not a string:

```
regreen.yml: checks[0].run: must be a string (quote the value)
```

Several problems in one file are reported together:

```
regreen.yml: policy.protected_files[2]: must be a string (quote the value)
regreen.yml: policy.protected_files[0]: "!a": negation with ! is not supported: a pattern can add protection, never remove it
regreen.yml: policy.protected_files[1]: "a//b": pattern has an empty path segment
regreen.yml: policy.max_attempts: 0 must be between 1 and 1000000
regreen.yml: policy.edit_scope: "some" must be "files_in_errors" or "any"
```

An unknown key, an anchor, an empty file, a missing check list, and a second document:

```
regreen.yml: yaml: construct errors: line 4: field bogus not found in type config.rawRepo
regreen.yml: anchors and aliases are not allowed
regreen.yml is empty
regreen.yml: checks: at least one check is required
regreen.yml must contain exactly one YAML document
```

(The unknown-key message comes from the YAML library and names an internal type; the line number is the useful part.)

## User config

A per-user file, separate from the repository. It holds preferences that should not live in a shared repo.

**Status:** its shape is provisional until M6, the first milestone that uses it (decision D20). Nothing reads it yet.

Location: `regreen/config.yml` inside the directory Go's `os.UserConfigDir` reports:

| System | Directory | File |
|---|---|---|
| Linux and most other Unix systems | `$XDG_CONFIG_HOME`, or `$HOME/.config` if it is not set | `<dir>/regreen/config.yml` |
| macOS | `$HOME/Library/Application Support` | `~/Library/Application Support/regreen/config.yml` |
| Windows | `%AppData%` | `%AppData%\regreen\config.yml` |

`config.UserConfigPath()` returns this path; it returns an error if Go cannot determine the directory.

A missing file, an empty file, or a file holding only `null` means the defaults. Otherwise the same strict loading applies as for `regreen.yml` (one document, at most 1 MiB, unknown keys rejected, strings must be strings, no anchors, all problems reported together, each starting with the file's full path).

```yaml
agent: claude-code
watch:
  delivery: headless
```

| Key | Type | Default | Rule |
|---|---|---|---|
| `agent` | string | none | `claude-code`, `codex` or `other`. |
| `watch.delivery` | string | `notify` | `notify` (tell the developer), `session` (push to a running agent session), or `headless` (start a new headless run). |

An invalid file is reported like this, where `<path>` stands for the file's full path:

```
<path>: agent: "gemini" must be one of claude-code, codex, other
<path>: watch.delivery: "loud" must be one of notify, session, headless
```
