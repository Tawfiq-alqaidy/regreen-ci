package policy

import (
	"strings"
	"testing"
)

func TestMatchPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		pattern, path string
		want          bool
	}{
		{"tsconfig.json", "tsconfig.json", true},
		{"tsconfig.json", "packages/app/tsconfig.json", true},
		{"tsconfig.json", "tsconfig.json.bak", false},
		{"/tsconfig.json", "tsconfig.json", true},
		{"/tsconfig.json", "packages/app/tsconfig.json", false},
		{".eslintrc*", ".eslintrc", true},
		{".eslintrc*", ".eslintrc.json", true},
		{".eslintrc*", "web/.eslintrc.cjs", true},
		{".eslintrc*", "eslintrc.json", false},
		{".github/**", ".github/workflows/ci.yml", true},
		{".github/**", ".github", true},
		{".github/**", "x/.github/workflows/ci.yml", false},
		{".github", ".github/workflows/ci.yml", true},
		{".github", "x/.github/workflows/ci.yml", true},
		{"docs/api", "docs/api/v1/x.md", true},
		{"docs/api", "docs/apiary.md", false},
		{"src/**/gen/*.go", "src/gen/a.go", true},
		{"src/**/gen/*.go", "src/a/b/gen/a.go", true},
		{"src/**/gen/*.go", "src/a/b/gen/sub/a.go", false},
		{"**/secret.txt", "secret.txt", true},
		{"**/secret.txt", "a/b/secret.txt", true},
		{"*.lock", "yarn.lock", true},
		{"*.lock", "a/b/c.lock", true},
		{"a/*.go", "a/b/c.go", false},
		{"node_modules", "a/node_modules/x/y.js", true},
		{"Tsconfig.json", "tsconfig.json", false},
		{"dir/", "dir/file", true},
		{"a[", "a[", false},
		{"", "a", false},
		{"!a", "a", false},
		{"/a/b/", "a/b/c", true},
		{"/a/b", "x/a/b", false},
		{"**", "a", true},
		{"**", "a/b/c", true},
		{"/**", "a", true},
		{"/**", "a/b/c", true},
		{"a/**/**/b", "a/b", true},
		{"a/**/**/b", "a/x/y/b", true},
		{"a/**/**/b", "a/x/b/c", true},
		{"a/**/**/b", "a/x/c", false},
		{"a/**/**/b", "x/a/b", false},
		{"**/**", "a", true},
		{"**/**", "a/b/c", true},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+"|"+tt.path, func(t *testing.T) {
			t.Parallel()
			if got := MatchPath(tt.pattern, tt.path); got != tt.want {
				t.Errorf("MatchPath(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
			}
		})
	}
}

func TestValidatePattern(t *testing.T) {
	t.Parallel()
	valid := []string{"tsconfig.json", "/a/b", ".github/**", "src/**/x", "*.lock", "a/[abc]?.go", "dir/"}
	for _, p := range valid {
		if err := ValidatePattern(p); err != nil {
			t.Errorf("ValidatePattern(%q) = %v, want nil", p, err)
		}
	}
	invalid := []string{
		"", "  ", "!regreen.yml", `a\b`, "a\x00b", "a\nb", "a/../b", "./a", "a/./b", "..",
		"a**b", "**a", "a[", "/", "a//b", "//a",
		" tsconfig.json", "tsconfig.json ", "\ttsconfig.json", "dir/ ", "a\r",
		"[!a]*.go", "src/[!x]", "a[!b]c",
	}
	for _, p := range invalid {
		if err := ValidatePattern(p); err == nil {
			t.Errorf("ValidatePattern(%q) = nil, want error", p)
		}
	}
}

func TestValidatePatternMessages(t *testing.T) {
	t.Parallel()
	tests := []struct{ pattern, want string }{
		{"[!a]*.go", "[^...]"},
		{" a", "whitespace"},
		{"a ", "whitespace"},
	}
	for _, tt := range tests {
		err := ValidatePattern(tt.pattern)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("ValidatePattern(%q) = %v, want an error mentioning %q", tt.pattern, err, tt.want)
		}
	}
	// A ! that does not open a class is an ordinary character.
	for _, p := range []string{"a!b", "[a!]"} {
		if err := ValidatePattern(p); err != nil {
			t.Errorf("ValidatePattern(%q) = %v, want nil", p, err)
		}
	}
	if !MatchPath("[^a]*.go", "b.go") || MatchPath("[^a]*.go", "a.go") {
		t.Error("[^a] class does not negate")
	}
}
