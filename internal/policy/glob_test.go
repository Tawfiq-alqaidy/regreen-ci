package policy

import "testing"

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
	}
	for _, p := range invalid {
		if err := ValidatePattern(p); err == nil {
			t.Errorf("ValidatePattern(%q) = nil, want error", p)
		}
	}
}
