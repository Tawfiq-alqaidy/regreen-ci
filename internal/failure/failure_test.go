package failure

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Tawfiq-alqaidy/regreen-ci/internal/ci"
	"github.com/Tawfiq-alqaidy/regreen-ci/internal/config"
)

func TestBuildMapped(t *testing.T) {
	step := ci.FailedStep{ID: "id-1", Workflow: "CI", Job: "build", Step: "Run npm run lint", StepNumber: 4, URL: "https://ci.example/1"}
	checks := []config.Check{{Name: "lint", Run: "npm run lint", Fix: "npm run lint -- --fix"}}
	log := "\x1b[31m/home/runner/work/app/app/src/a.ts:12:5\x1b[0m error\nsee main.go:3"
	got := Build(ci.StepLog{Step: step, Commit: "abc123", Log: log}, checks, knownSet(), DefaultLimits())
	want := Failure{
		ID: "id-1", Commit: "abc123", Workflow: "CI", Job: "build", Step: "Run npm run lint", URL: "https://ci.example/1",
		Check: "lint", Command: "npm run lint", Fixable: true,
		Errors: "/home/runner/work/app/app/src/a.ts:12:5 error\nsee main.go:3",
		Files:  []string{"main.go", "src/a.ts"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestBuildUnmapped(t *testing.T) {
	step := ci.FailedStep{ID: "id-2", Workflow: "CI", Job: "deploy", Step: "Run ./deploy.sh"}
	checks := []config.Check{{Name: "lint", Run: "npm run lint"}}
	got := Build(ci.StepLog{Step: step, Commit: "abc", Log: "boom\n"}, checks, knownSet(), DefaultLimits())
	if got.Fixable || got.Check != "" || got.Command != "" {
		t.Fatalf("unmapped failure must not be fixable: %+v", got)
	}
	if got.Errors != "boom" || got.ID != "id-2" || got.Files != nil {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestBuildFilesComeFromTrimmedText(t *testing.T) {
	log := "src/a.ts:1\n" + strings.Repeat("noise\n", 50) + "src/b.ts:2"
	got := Build(ci.StepLog{Commit: "c", Log: log}, nil, knownSet(), Limits{MaxLines: 5})
	if !got.Truncated {
		t.Fatal("expected truncation")
	}
	if !reflect.DeepEqual(got.Files, []string{"src/b.ts"}) {
		t.Fatalf("Files = %v, want only the file in the kept tail", got.Files)
	}
}

func TestBuildPartialMarksTruncated(t *testing.T) {
	tests := []struct {
		name    string
		sl      ci.StepLog
		lim     Limits
		want    bool
		wantErr string
	}{
		{"complete, fits", ci.StepLog{Log: "boom"}, DefaultLimits(), false, "boom"},
		{"partial, fits", ci.StepLog{Log: "boom", Partial: true}, DefaultLimits(), true, "boom"},
		{"partial, empty", ci.StepLog{Partial: true}, DefaultLimits(), true, ""},
		{"complete, trimmed", ci.StepLog{Log: "a\nb\nc"}, Limits{MaxLines: 1}, true, "[… 2 earlier lines trimmed …]\nc"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Build(tc.sl, nil, knownSet(), tc.lim)
			if got.Truncated != tc.want || got.Errors != tc.wantErr {
				t.Fatalf("Truncated = %v, Errors = %q; want %v, %q", got.Truncated, got.Errors, tc.want, tc.wantErr)
			}
		})
	}
}

// TestFailurePackageIsPure checks the non-test sources import only this
// module's ci and config packages and standard-library packages that do no
// I/O. os, os/exec, net and net/http must never appear.
func TestFailurePackageIsPure(t *testing.T) {
	const mod = "github.com/Tawfiq-alqaidy/regreen-ci/"
	moduleOK := []string{mod + "internal/ci", mod + "internal/config"}
	denied := []string{"os", "os/exec", "net", "net/http"}
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
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			first, _, _ := strings.Cut(p, "/")
			stdlib := !strings.Contains(first, ".")
			if slices.Contains(denied, p) || (!stdlib && !slices.Contains(moduleOK, p)) {
				t.Errorf("%s imports %q, which is not allowed in a pure package", name, p)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no non-test sources found")
	}
}
