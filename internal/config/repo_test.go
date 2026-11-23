package config

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Tawfiq-alqaidy/regreen-ci/internal/policy"
)

const fullRepoYAML = `
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
`

const minimalRepoYAML = "checks:\n  - name: lint\n    run: make lint\n"

func TestParseRepoFull(t *testing.T) {
	got, err := ParseRepo([]byte(fullRepoYAML))
	if err != nil {
		t.Fatalf("ParseRepo: %v", err)
	}
	want := &Repo{
		Checks: []Check{
			{Name: "lint", Run: "npm run lint", Fix: "npx eslint --fix ."},
			{Name: "test", Run: "npm test"},
		},
		Policy: policy.Rules{
			ProtectedFiles:    []string{".eslintrc*", "tsconfig.json"},
			ForbiddenPatterns: []string{"eslint-disable", "@ts-ignore", "@ts-expect-error"},
			MaxChangedLines:   200,
			MaxDeletedLines:   50,
			MaxAttempts:       2,
			EditScope:         policy.ScopeFilesInErrors,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestParseRepoDefaults(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want func(*policy.Rules)
	}{
		{"no policy key", minimalRepoYAML, func(*policy.Rules) {}},
		{"explicit zero deleted lines", minimalRepoYAML + "policy:\n  max_deleted_lines: 0\n",
			func(r *policy.Rules) { r.MaxDeletedLines = 0 }},
		{"scope any", minimalRepoYAML + "policy:\n  edit_scope: any\n",
			func(r *policy.Rules) { r.EditScope = policy.ScopeAny }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRepo([]byte(tt.yaml))
			if err != nil {
				t.Fatalf("ParseRepo: %v", err)
			}
			want := policy.DefaultRules()
			tt.want(&want)
			if !reflect.DeepEqual(got.Policy, want) {
				t.Errorf("policy = %+v, want %+v", got.Policy, want)
			}
		})
	}
}

func TestParseRepoInvalid(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{"empty input", "", "regreen.yml is empty"},
		{"null document", "null\n", "regreen.yml is empty"},
		{"no checks key", "policy: {}\n", "checks"},
		{"empty checks", "checks: []\n", "checks"},
		{"missing name", "checks:\n  - run: x\n", "checks[0].name"},
		{"missing run", "checks:\n  - name: a\n", "checks[0].run"},
		{"empty fix", "checks:\n  - name: a\n    run: x\n    fix: \"\"\n", "checks[0].fix"},
		{"duplicate names", "checks:\n  - name: a\n    run: x\n  - name: a\n    run: y\n", "checks[1].name"},
		{"name with space", "checks:\n  - name: \"a b\"\n    run: x\n", "checks[0].name"},
		{"name leading dash", "checks:\n  - name: \"-a\"\n    run: x\n", "checks[0].name"},
		{"unknown top-level key", "cheks: []\n", "cheks"},
		{"unknown check key", "checks:\n  - name: a\n    run: x\n    cmd: y\n", "cmd"},
		{"unknown policy key", minimalRepoYAML + "policy:\n  max_lines: 3\n", "max_lines"},
		{"max_changed_lines 0", minimalRepoYAML + "policy:\n  max_changed_lines: 0\n", "policy.max_changed_lines"},
		{"max_deleted_lines negative", minimalRepoYAML + "policy:\n  max_deleted_lines: -1\n", "policy.max_deleted_lines"},
		{"max_attempts 0", minimalRepoYAML + "policy:\n  max_attempts: 0\n", "policy.max_attempts"},
		{"max_attempts too big", minimalRepoYAML + "policy:\n  max_attempts: 9999999\n", "policy.max_attempts"},
		{"bad edit_scope", minimalRepoYAML + "policy:\n  edit_scope: everything\n", "policy.edit_scope"},
		{"negated protected pattern", minimalRepoYAML + "policy:\n  protected_files: [\"!regreen.yml\"]\n", "policy.protected_files[0]"},
		{"empty protected pattern", minimalRepoYAML + "policy:\n  protected_files: [\"\"]\n", "policy.protected_files[0]"},
		{"empty forbidden pattern", minimalRepoYAML + "policy:\n  forbidden_patterns: [\"\"]\n", "policy.forbidden_patterns[0]"},
		{"newline in forbidden pattern", minimalRepoYAML + "policy:\n  forbidden_patterns: [\"a\\nb\"]\n", "policy.forbidden_patterns[0]"},
		{"NUL in run", "checks:\n  - name: a\n    run: \"x\\u0000y\"\n", "checks[0].run"},
		{"NUL in fix", "checks:\n  - name: a\n    run: x\n    fix: \"x\\u0000y\"\n", "checks[0].fix"},
		{"second document", minimalRepoYAML + "---\n" + minimalRepoYAML, "document"},
		{"non-mapping document", "- a\n", "regreen.yml"},
		{"string where int expected", minimalRepoYAML + "policy:\n  max_attempts: \"two\"\n", "cannot construct"},
		{"malformed yaml", "checks: [\n", "regreen.yml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRepo([]byte(tt.yaml))
			if err == nil {
				t.Fatalf("expected error, got %+v", got)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

func TestParseRepoReportsAllProblems(t *testing.T) {
	y := "checks:\n  - name: \"bad name\"\n    run: \"\"\npolicy:\n  max_attempts: 0\n"
	_, err := ParseRepo([]byte(y))
	if err == nil {
		t.Fatal("expected error")
	}
	for _, p := range []string{"checks[0].name", "checks[0].run", "policy.max_attempts"} {
		if !strings.Contains(err.Error(), p) {
			t.Errorf("error %q missing %q", err, p)
		}
	}
}

func TestParseRepoTooLarge(t *testing.T) {
	data := []byte(minimalRepoYAML + "#" + strings.Repeat("a", maxConfigBytes))
	_, err := ParseRepo(data)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err = %v, want size error", err)
	}
	if _, err := ParseRepo(data[:maxConfigBytes]); err != nil {
		t.Errorf("input of exactly the cap should parse: %v", err)
	}
}

func TestParseRepoDoesNotRemoveHardcodedProtection(t *testing.T) {
	repo, err := ParseRepo([]byte(minimalRepoYAML + "policy:\n  protected_files: [\"src/gen\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"regreen.yml", ".github/workflows/ci.yml", "src/gen"} {
		if _, ok := policy.IsProtected(p, repo.Policy.ProtectedFiles); !ok {
			t.Errorf("%s should be protected", p)
		}
	}
}

func TestParsedRulesAreAcceptedByEvaluate(t *testing.T) {
	diff := policy.Diff{Files: []policy.FileChange{{
		Path: "src/a.go", Kind: policy.Modified, Deleted: 1,
		Added: []policy.Line{{Number: 1, Text: "x"}},
	}}}
	for name, y := range map[string]string{"full": fullRepoYAML, "defaults": minimalRepoYAML} {
		t.Run(name, func(t *testing.T) {
			repo, err := ParseRepo([]byte(y))
			if err != nil {
				t.Fatal(err)
			}
			v := policy.Evaluate(policy.Input{
				Diff: diff, Rules: repo.Policy, ErrorFiles: []string{"src/a.go"},
			})
			for _, r := range v.Reasons {
				if r.Rule == policy.RuleInvalidRules {
					t.Errorf("invalid_rules reason: %+v", r)
				}
			}
		})
	}
}

func TestLoadRepo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, RepoFileName)
	if err := os.WriteFile(path, []byte(fullRepoYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	repo, err := LoadRepo(context.Background(), path)
	if err != nil || len(repo.Checks) != 2 {
		t.Fatalf("LoadRepo = %+v, %v", repo, err)
	}

	missing := filepath.Join(dir, "nope.yml")
	_, err = LoadRepo(context.Background(), missing)
	if !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), missing) {
		t.Errorf("missing file err = %v", err)
	}

	bad := filepath.Join(dir, "bad.yml")
	if err := os.WriteFile(bad, []byte("checks: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRepo(context.Background(), bad); err == nil || !strings.HasPrefix(err.Error(), bad+": ") {
		t.Errorf("parse error should be prefixed with path, got %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := LoadRepo(ctx, path); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled ctx err = %v", err)
	}
}
