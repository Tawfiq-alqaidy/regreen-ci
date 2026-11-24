package failure

import (
	"testing"

	"github.com/Tawfiq-alqaidy/regreen-ci/internal/config"
)

func TestMatchCheck(t *testing.T) {
	checks := []config.Check{
		{Name: "lint", Run: "npm run lint"},
		{Name: "types", Run: "npx tsc --noEmit"},
		{Name: "fmt", Run: "npm run"},
	}
	tests := []struct {
		name, job, step string
		checks          []config.Check
		want            string // check name, "" for no match
	}{
		{"exact", "build", "Run npm run lint", checks, "lint"},
		{"whitespace", "build", "Run npm   run lint ", checks, "lint"},
		{"longest contained wins", "build", "Run npm run lint -- --max-warnings 0", checks, "lint"},
		{"shorter exact", "build", "Run npm run", checks, "fmt"},
		{"name case-insensitive", "build", "Lint", checks, "lint"},
		{"job fallback", "types", "Check code", checks, "types"},
		{"token boundary", "build", "Run xnpm run lint", checks, ""},
		{"trailing boundary", "build", "Run npm run linter", checks, "fmt"},
		{"no match", "build", "Run go test ./...", checks, ""},
		{"job only", "lint", "", checks, "lint"},
		{"no checks", "lint", "Run npm run lint", nil, ""},
		{"no Run prefix", "build", "npm run lint", checks, "lint"},
		{"command contained mid-string", "build", "Run cd app && npm run lint", checks, "lint"},
		{"ambiguous same run", "build", "Run make", []config.Check{{Name: "a", Run: "make"}, {Name: "b", Run: "make"}}, ""},
		{"ambiguous contained tie", "build", "Run make all", []config.Check{{Name: "a", Run: "make"}, {Name: "b", Run: "make"}}, ""},
		{"ambiguity does not fall to lower level", "a", "Run make", []config.Check{{Name: "a", Run: "make"}, {Name: "b", Run: "make"}}, ""},
		{"empty Run never matches by command", "build", "Run anything", []config.Check{{Name: "x", Run: ""}}, ""},
		{"empty step skips command levels", "build", "", []config.Check{{Name: "x", Run: "build"}}, ""},
		{"level 1 beats job", "lint", "Run npx tsc --noEmit", checks, "types"},
		{"level 2 beats job", "lint", "Run npx tsc --noEmit --pretty", checks, "types"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := MatchCheck(tc.job, tc.step, tc.checks)
			if tc.want == "" {
				if ok || got != (config.Check{}) {
					t.Fatalf("got (%+v, %v), want no match", got, ok)
				}
				return
			}
			if !ok || got.Name != tc.want {
				t.Fatalf("got (%+v, %v), want %q", got, ok, tc.want)
			}
		})
	}
}
