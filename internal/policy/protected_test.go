package policy

import (
	"slices"
	"testing"
)

func TestAlwaysProtected(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want bool
	}{
		{"regreen.yml", true},
		{"regreen.yaml", true},
		{"REGREEN.YML", true},
		{"sub/regreen.yml", true},
		{".github/workflows/ci.yml", true},
		{".GitHub/workflows/ci.yml", true},
		{".github", true},
		{".regreen/state.json", true},
		{".git/hooks/pre-push", true},
		{".githooks/pre-push", true},
		{".husky/pre-commit", true},
		{".pre-commit-config.yaml", true},
		{"lefthook.yml", true},
		{".lefthook.yml", true},
		{"src/main.go", false},
		{"regreen.yml.md", false},
		{"github/x", false},
		{"docs/regreen.md", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			pat, got := IsProtected(tt.path, nil)
			if got != tt.want {
				t.Fatalf("IsProtected(%q) = %v, want %v", tt.path, got, tt.want)
			}
			if got != (pat != "") {
				t.Errorf("IsProtected(%q) pattern %q inconsistent with result %v", tt.path, pat, got)
			}
		})
	}
}

func TestIsProtectedExtra(t *testing.T) {
	t.Parallel()
	extra := []string{"tsconfig.json", "!regreen.yml", ".eslintrc*"}
	tests := []struct {
		path        string
		wantPattern string
		want        bool
	}{
		{"packages/a/tsconfig.json", "tsconfig.json", true},
		{".eslintrc.json", ".eslintrc*", true},
		{"regreen.yml", "regreen.yml", true},
		{"src/a.ts", "", false},
		{"TSCONFIG.JSON", "", false},
	}
	for _, tt := range tests {
		pat, got := IsProtected(tt.path, extra)
		if got != tt.want || pat != tt.wantPattern {
			t.Errorf("IsProtected(%q) = (%q, %v), want (%q, %v)", tt.path, pat, got, tt.wantPattern, tt.want)
		}
	}
}

func TestAlwaysProtectedReturnsCopy(t *testing.T) {
	t.Parallel()
	first := AlwaysProtected()
	want := slices.Clone(first)
	if len(first) == 0 {
		t.Fatal("AlwaysProtected returned no patterns")
	}
	for i := range first {
		first[i] = "zzz"
	}
	if got := AlwaysProtected(); !slices.Equal(got, want) {
		t.Errorf("second call = %v, want %v", got, want)
	}
	if _, ok := IsProtected("regreen.yml", nil); !ok {
		t.Error("mutating the returned slice removed protection")
	}
}
