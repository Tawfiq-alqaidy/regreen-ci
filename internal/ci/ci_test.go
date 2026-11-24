package ci

import (
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	goparser "go/parser"
)

func TestValidCommit(t *testing.T) {
	h40 := strings.Repeat("a1", 20)
	h64 := strings.Repeat("0f", 32)
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"40 hex", h40, true},
		{"64 hex", h64, true},
		{"39 chars", h40[:39], false},
		{"41 chars", h40 + "a", false},
		{"upper case", strings.ToUpper(h40), false},
		{"symbolic HEAD", "HEAD", false},
		{"empty", "", false},
		{"40 chars with g", h40[:39] + "g", false},
		{"40 chars with space", h40[:39] + " ", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidCommit(tt.in); got != tt.want {
				t.Errorf("ValidCommit(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestRepoString(t *testing.T) {
	got := Repo{Host: "github.com", Owner: "o", Name: "r"}.String()
	if want := "github.com/o/r"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// TestCIPackageIsPure checks the non-test sources import only the listed
// standard-library packages. None of them does I/O. A new import must be added
// here deliberately; os, os/exec, net and net/http must never appear.
func TestCIPackageIsPure(t *testing.T) {
	allowed := []string{"context", "errors", "net/url", "strings"}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		checked++
		f, err := goparser.ParseFile(token.NewFileSet(), name, nil, goparser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range f.Imports {
			if p := strings.Trim(imp.Path.Value, `"`); !slices.Contains(allowed, p) {
				t.Errorf("%s imports %q, which is not on the purity allowlist", name, p)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no non-test sources found")
	}
}
