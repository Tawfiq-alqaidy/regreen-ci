package policy

import (
	"strings"
	"testing"
)

func TestCheckRepoPath(t *testing.T) {
	t.Parallel()
	segments := func(n int) string { return strings.TrimSuffix(strings.Repeat("a/", n), "/") }
	valid := []string{
		"a.go",
		"src/a.go",
		"my file.txt",
		"café.txt",
		strings.Repeat("x", maxPathBytes),
		segments(maxPathSegments),
	}
	for _, p := range valid {
		if err := checkRepoPath(p); err != nil {
			t.Errorf("checkRepoPath(%q) = %v, want nil", clip(p), err)
		}
	}
	invalid := []struct{ name, path string }{
		{"empty", ""},
		{"absolute", "/etc/passwd"},
		{"dot", "."},
		{"dot dot", ".."},
		{"parent", "../x"},
		{"dot dot inside", "a/../b"},
		{"unclean", "a//b"},
		{"trailing slash", "a/"},
		{"backslash", `a\b`},
		{"NUL", "a\x00b"},
		{"tab", "a\tb"},
		{"newline", "a\nb"},
		{"carriage return", "a\rb"},
		{"escape sequence", "a\x1b[31mb"},
		{"unit separator", "a\x1fb"},
		{"DEL", "a\x7fb"},
		{"invalid UTF-8", "caf\xe9.txt"},
		{"truncated UTF-8", "caf\xc3"},
		{"too long", strings.Repeat("x", maxPathBytes+1)},
		{"too many segments", segments(maxPathSegments + 1)},
	}
	for _, tt := range invalid {
		if err := checkRepoPath(tt.path); err == nil {
			t.Errorf("%s: checkRepoPath(%q) = nil, want error", tt.name, clip(tt.path))
		}
	}
}

func TestPathLimitsAppliedByParserAndEvaluate(t *testing.T) {
	t.Parallel()
	modeOnly := func(p string) string {
		return "diff --git a/" + p + " b/" + p + "\nold mode 100644\nnew mode 100755\n"
	}
	segments := func(n int) string { return strings.TrimSuffix(strings.Repeat("a/", n), "/") }
	tests := []struct {
		name string
		path string
		ok   bool
	}{
		{"longest allowed path", strings.Repeat("x", maxPathBytes), true},
		{"most segments allowed", segments(maxPathSegments), true},
		{"path too long", strings.Repeat("x", maxPathBytes+1), false},
		{"too many segments", segments(maxPathSegments + 1), false},
		{"raw escape sequence", "a\x1b[2Kb", false},
		{"raw DEL", "a\x7fb", false},
		{"raw invalid UTF-8", "caf\xe9", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseUnifiedDiff(modeOnly(tt.path))
			if (err == nil) != tt.ok {
				t.Errorf("ParseUnifiedDiff error = %v, want ok=%v", err, tt.ok)
			}
			v := Evaluate(Input{
				Diff:  Diff{Files: []FileChange{mod(tt.path, 0, "x")}},
				Rules: rulesWith(func(r *Rules) { r.EditScope = ScopeAny }),
			})
			gotInvalid := len(v.Reasons) > 0 && v.Reasons[0].Rule == RuleInvalidPath
			if gotInvalid == tt.ok {
				t.Errorf("Evaluate reasons = %v, want invalid_path=%v", ruleNames(v), !tt.ok)
			}
		})
	}
}
