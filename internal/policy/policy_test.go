package policy

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// mod builds a modified file with `deleted` removed lines and the given added
// lines, numbered from 1.
func mod(p string, deleted int, added ...string) FileChange {
	fc := FileChange{Path: p, Kind: Modified, Deleted: deleted}
	for i, text := range added {
		fc.Added = append(fc.Added, Line{Number: i + 1, Text: text})
	}
	return fc
}

func repeat(text string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = text
	}
	return out
}

func rulesWith(f func(*Rules)) Rules {
	r := DefaultRules()
	f(&r)
	return r
}

func TestDefaultRules(t *testing.T) {
	want := Rules{MaxChangedLines: 200, MaxDeletedLines: 50, MaxAttempts: 2, EditScope: ScopeFilesInErrors}
	if got := DefaultRules(); !reflect.DeepEqual(got, want) {
		t.Fatalf("DefaultRules() = %+v, want %+v", got, want)
	}
}

func TestEvaluate(t *testing.T) {
	oneFile := []string{"src/a.ts"}
	tests := []struct {
		name          string
		in            Input
		wantAllowed   bool
		wantRules     []string
		wantRemaining int
		check         func(t *testing.T, v Verdict)
	}{
		{
			name:          "small change to named file is allowed",
			in:            Input{Diff: Diff{Files: []FileChange{mod("src/a.ts", 1, "x")}}, Rules: DefaultRules(), ErrorFiles: oneFile},
			wantAllowed:   true,
			wantRemaining: 2,
		},
		{
			name:          "file not in errors",
			in:            Input{Diff: Diff{Files: []FileChange{mod("src/b.ts", 0, "x")}}, Rules: DefaultRules(), ErrorFiles: oneFile},
			wantRules:     []string{RuleEditScope},
			wantRemaining: 2,
			check: func(t *testing.T, v Verdict) {
				t.Helper()
				if v.Reasons[0].File != "src/b.ts" {
					t.Errorf("File = %q", v.Reasons[0].File)
				}
				want := "src/b.ts is not named in the errors; only these files may be edited: src/a.ts"
				if v.Reasons[0].Message != want {
					t.Errorf("Message = %q, want %q", v.Reasons[0].Message, want)
				}
			},
		},
		{
			name: "empty ErrorFiles rejects every file",
			in: Input{Diff: Diff{Files: []FileChange{mod("src/a.ts", 0, "x"), mod("src/b.ts", 0, "y")}},
				Rules: DefaultRules()},
			wantRules:     []string{RuleEditScope, RuleEditScope},
			wantRemaining: 2,
			check: func(t *testing.T, v Verdict) {
				t.Helper()
				if !strings.Contains(v.Reasons[0].Message, "no files are named in the errors") {
					t.Errorf("Message = %q", v.Reasons[0].Message)
				}
			},
		},
		{
			name: "scope any allows unnamed file",
			in: Input{Diff: Diff{Files: []FileChange{mod("src/b.ts", 0, "x")}},
				Rules: rulesWith(func(r *Rules) { r.EditScope = ScopeAny }), ErrorFiles: oneFile},
			wantAllowed: true, wantRemaining: 2,
		},
		{
			name:        "error file with ./ prefix matches",
			in:          Input{Diff: Diff{Files: []FileChange{mod("src/a.ts", 0, "x")}}, Rules: DefaultRules(), ErrorFiles: []string{"./src/a.ts"}},
			wantAllowed: true, wantRemaining: 2,
		},
		{
			name:          "error file differing in case does not match",
			in:            Input{Diff: Diff{Files: []FileChange{mod("src/a.ts", 0, "x")}}, Rules: DefaultRules(), ErrorFiles: []string{"src/A.ts"}},
			wantRules:     []string{RuleEditScope},
			wantRemaining: 2,
		},
		{
			name: "edit scope message lists files sorted",
			in: Input{Diff: Diff{Files: []FileChange{mod("z.ts", 0, "x")}}, Rules: DefaultRules(),
				ErrorFiles: []string{"src/b.ts", "src/a.ts"}},
			wantRules: []string{RuleEditScope}, wantRemaining: 2,
			check: func(t *testing.T, v Verdict) {
				t.Helper()
				if !strings.HasSuffix(v.Reasons[0].Message, "src/a.ts, src/b.ts") {
					t.Errorf("Message = %q", v.Reasons[0].Message)
				}
			},
		},
		{
			name: "regreen.yml is always protected",
			in: Input{Diff: Diff{Files: []FileChange{mod("regreen.yml", 0, "x")}},
				Rules: rulesWith(func(r *Rules) { r.EditScope = ScopeAny }), ErrorFiles: []string{"regreen.yml"}},
			wantRules: []string{RuleProtectedFile}, wantRemaining: 2,
			check: func(t *testing.T, v Verdict) {
				t.Helper()
				if !strings.Contains(v.Reasons[0].Message, "always protected") || !strings.Contains(v.Reasons[0].Message, "regreen.yml") {
					t.Errorf("Message = %q", v.Reasons[0].Message)
				}
			},
		},
		{
			name: "workflow file is protected",
			in: Input{Diff: Diff{Files: []FileChange{mod(".github/workflows/ci.yml", 0, "x")}},
				Rules: rulesWith(func(r *Rules) { r.EditScope = ScopeAny })},
			wantRules: []string{RuleProtectedFile}, wantRemaining: 2,
		},
		{
			name: "deleting a workflow file is protected",
			in: Input{Diff: Diff{Files: []FileChange{{Path: ".github/workflows/ci.yml", Kind: Deleted, Deleted: 5}}},
				Rules: rulesWith(func(r *Rules) { r.EditScope = ScopeAny })},
			wantRules: []string{RuleProtectedFile}, wantRemaining: 2,
		},
		{
			name: "config protected file",
			in: Input{Diff: Diff{Files: []FileChange{mod("packages/a/tsconfig.json", 0, "x")}},
				Rules: rulesWith(func(r *Rules) { r.EditScope = ScopeAny; r.ProtectedFiles = []string{"tsconfig.json"} })},
			wantRules: []string{RuleProtectedFile}, wantRemaining: 2,
			check: func(t *testing.T, v Verdict) {
				t.Helper()
				if !strings.Contains(v.Reasons[0].Message, "project config") || !strings.Contains(v.Reasons[0].Message, "tsconfig.json") {
					t.Errorf("Message = %q", v.Reasons[0].Message)
				}
			},
		},
		{
			name: "negation in config cannot unprotect",
			in: Input{Diff: Diff{Files: []FileChange{mod("regreen.yml", 0, "x")}},
				Rules: rulesWith(func(r *Rules) { r.EditScope = ScopeAny; r.ProtectedFiles = []string{"!regreen.yml"} })},
			wantRules: []string{RuleProtectedFile}, wantRemaining: 2,
		},
		{
			name: "forbidden pattern on added line",
			in: Input{Diff: Diff{Files: []FileChange{mod("src/a.ts", 0, "ok", "// eslint-disable-next-line")}},
				Rules:      rulesWith(func(r *Rules) { r.ForbiddenPatterns = []string{"eslint-disable"} }),
				ErrorFiles: oneFile},
			wantRules: []string{RuleForbiddenPattern}, wantRemaining: 2,
			check: func(t *testing.T, v Verdict) {
				t.Helper()
				r := v.Reasons[0]
				if r.File != "src/a.ts" || r.Line != 2 {
					t.Errorf("File/Line = %q/%d", r.File, r.Line)
				}
				if r.Message != `added line contains forbidden pattern "eslint-disable"` {
					t.Errorf("Message = %q", r.Message)
				}
			},
		},
		{
			name: "removing a forbidden pattern is allowed",
			in: Input{Diff: Diff{Files: []FileChange{mod("src/a.ts", 1)}},
				Rules:      rulesWith(func(r *Rules) { r.ForbiddenPatterns = []string{"eslint-disable"} }),
				ErrorFiles: oneFile},
			wantAllowed: true, wantRemaining: 2,
		},
		{
			name: "forbidden patterns are case sensitive",
			in: Input{Diff: Diff{Files: []FileChange{mod("src/a.ts", 0, "// ESLINT-DISABLE")}},
				Rules:      rulesWith(func(r *Rules) { r.ForbiddenPatterns = []string{"eslint-disable"} }),
				ErrorFiles: oneFile},
			wantAllowed: true, wantRemaining: 2,
		},
		{
			name: "one line matching two patterns gives two reasons",
			in: Input{Diff: Diff{Files: []FileChange{mod("src/a.ts", 0, "// @ts-ignore eslint-disable")}},
				Rules:      rulesWith(func(r *Rules) { r.ForbiddenPatterns = []string{"eslint-disable", "@ts-ignore"} }),
				ErrorFiles: oneFile},
			wantRules: []string{RuleForbiddenPattern, RuleForbiddenPattern}, wantRemaining: 2,
		},
		{
			name: "forbidden pattern reasons are capped at 20 plus summary",
			in: Input{Diff: Diff{Files: []FileChange{mod("src/a.ts", 0, repeat("eslint-disable", 25)...)}},
				Rules:      rulesWith(func(r *Rules) { r.ForbiddenPatterns = []string{"eslint-disable"} }),
				ErrorFiles: oneFile},
			wantRules: repeat(RuleForbiddenPattern, 21), wantRemaining: 2,
			check: func(t *testing.T, v Verdict) {
				t.Helper()
				last := v.Reasons[20]
				if last.File != "" || last.Line != 0 || last.Message != "and 5 more added lines contain forbidden patterns" {
					t.Errorf("summary = %+v", last)
				}
			},
		},
		{
			name:          "51 deleted lines exceed the default",
			in:            Input{Diff: Diff{Files: []FileChange{mod("src/a.ts", 51)}}, Rules: DefaultRules(), ErrorFiles: oneFile},
			wantRules:     []string{RuleMaxDeletedLines},
			wantRemaining: 2,
			check: func(t *testing.T, v Verdict) {
				t.Helper()
				if !strings.Contains(v.Reasons[0].Message, "51") || !strings.Contains(v.Reasons[0].Message, "50") {
					t.Errorf("Message = %q", v.Reasons[0].Message)
				}
			},
		},
		{
			name:        "exactly 50 deleted lines is allowed",
			in:          Input{Diff: Diff{Files: []FileChange{mod("src/a.ts", 50)}}, Rules: DefaultRules(), ErrorFiles: oneFile},
			wantAllowed: true, wantRemaining: 2,
		},
		{
			name: "both totals exceeded, deleted first",
			in: Input{Diff: Diff{Files: []FileChange{mod("src/a.ts", 51, repeat("x", 75)...), mod("src/b.ts", 0, repeat("y", 75)...)}},
				Rules: DefaultRules(), ErrorFiles: []string{"src/a.ts", "src/b.ts"}},
			wantRules: []string{RuleMaxDeletedLines, RuleMaxChangedLines}, wantRemaining: 2,
			check: func(t *testing.T, v Verdict) {
				t.Helper()
				if !strings.Contains(v.Reasons[1].Message, "201") || !strings.Contains(v.Reasons[1].Message, "200") {
					t.Errorf("Message = %q", v.Reasons[1].Message)
				}
			},
		},
		{
			name: "200 changed lines is inclusive",
			in: Input{Diff: Diff{Files: []FileChange{mod("src/a.ts", 50, repeat("x", 75)...), mod("src/b.ts", 0, repeat("y", 75)...)}},
				Rules: DefaultRules(), ErrorFiles: []string{"src/a.ts", "src/b.ts"}},
			wantAllowed: true, wantRemaining: 2,
		},
		{
			name: "zero deletions permitted rejects one deletion",
			in: Input{Diff: Diff{Files: []FileChange{mod("src/a.ts", 1)}},
				Rules:      rulesWith(func(r *Rules) { r.MaxDeletedLines = 0 }),
				ErrorFiles: oneFile},
			wantRules: []string{RuleMaxDeletedLines}, wantRemaining: 2,
		},
		{
			name: "zero deletions permitted allows none",
			in: Input{Diff: Diff{Files: []FileChange{mod("src/a.ts", 0, "x")}},
				Rules:      rulesWith(func(r *Rules) { r.MaxDeletedLines = 0 }),
				ErrorFiles: oneFile},
			wantAllowed: true, wantRemaining: 2,
		},
		{
			name: "binary file",
			in: Input{Diff: Diff{Files: []FileChange{{Path: "src/a.ts", Binary: true}}},
				Rules: DefaultRules(), ErrorFiles: oneFile},
			wantRules: []string{RuleBinaryFile}, wantRemaining: 2,
		},
		{
			name: "special file",
			in: Input{Diff: Diff{Files: []FileChange{{Path: "src/a.ts", Special: "symlink"}}},
				Rules: DefaultRules(), ErrorFiles: oneFile},
			wantRules: []string{RuleSpecialFile}, wantRemaining: 2,
			check: func(t *testing.T, v Verdict) {
				t.Helper()
				if !strings.Contains(v.Reasons[0].Message, "symlink") {
					t.Errorf("Message = %q", v.Reasons[0].Message)
				}
			},
		},
		{
			name: "empty diff", in: Input{Rules: DefaultRules(), ErrorFiles: oneFile},
			wantRules: []string{RuleEmptyDiff}, wantRemaining: 2,
		},
		{
			name: "attempts exhausted",
			in: Input{Diff: Diff{Files: []FileChange{mod("src/a.ts", 0, "x")}}, Rules: DefaultRules(),
				ErrorFiles: oneFile, AttemptsUsed: 2},
			wantRules: []string{RuleMaxAttempts}, wantRemaining: 0,
			check: func(t *testing.T, v Verdict) {
				t.Helper()
				want := "no attempts remaining (used 2 of 2); stop and report to the developer"
				if v.Reasons[0].Message != want {
					t.Errorf("Message = %q", v.Reasons[0].Message)
				}
			},
		},
		{
			name: "one attempt used leaves one",
			in: Input{Diff: Diff{Files: []FileChange{mod("src/a.ts", 0, "x")}}, Rules: DefaultRules(),
				ErrorFiles: oneFile, AttemptsUsed: 1},
			wantAllowed: true, wantRemaining: 1,
		},
		{
			name: "attempts over the limit clamp to zero",
			in: Input{Diff: Diff{Files: []FileChange{mod("src/a.ts", 0, "x")}}, Rules: DefaultRules(),
				ErrorFiles: oneFile, AttemptsUsed: 5},
			wantRules: []string{RuleMaxAttempts}, wantRemaining: 0,
		},
		{
			name: "all violations reported in documented order",
			in: Input{
				Diff: Diff{Files: []FileChange{
					mod("regreen.yml", 0, "x"),
					mod("src/b.ts", 60, "// eslint-disable"),
				}},
				Rules:        rulesWith(func(r *Rules) { r.ForbiddenPatterns = []string{"eslint-disable"} }),
				ErrorFiles:   oneFile,
				AttemptsUsed: 2,
			},
			wantRules: []string{
				RuleMaxAttempts,
				RuleProtectedFile, RuleEditScope, // regreen.yml
				RuleEditScope, RuleForbiddenPattern, // src/b.ts
				RuleMaxDeletedLines,
			},
			wantRemaining: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Evaluate(tt.in)
			if v.Allowed != tt.wantAllowed {
				t.Errorf("Allowed = %v, want %v (reasons %+v)", v.Allowed, tt.wantAllowed, v.Reasons)
			}
			if got := ruleNames(v); !slices.Equal(got, tt.wantRules) {
				t.Errorf("rules = %v, want %v", got, tt.wantRules)
			}
			if v.Allowed != (len(v.Reasons) == 0) {
				t.Errorf("Allowed %v inconsistent with %d reasons", v.Allowed, len(v.Reasons))
			}
			if v.AttemptsRemaining != tt.wantRemaining {
				t.Errorf("AttemptsRemaining = %d, want %d", v.AttemptsRemaining, tt.wantRemaining)
			}
			if tt.check != nil {
				tt.check(t, v)
			}
		})
	}
}

func ruleNames(v Verdict) []string {
	var out []string
	for _, r := range v.Reasons {
		out = append(out, r.Rule)
	}
	return out
}

func TestEvaluateInvalidRules(t *testing.T) {
	protectedDiff := Diff{Files: []FileChange{mod("regreen.yml", 99, "x")}}
	tests := []struct {
		name string
		edit func(in *Input)
	}{
		{"max changed lines zero", func(in *Input) { in.Rules.MaxChangedLines = 0 }},
		{"max deleted lines negative", func(in *Input) { in.Rules.MaxDeletedLines = -1 }},
		{"max attempts zero", func(in *Input) { in.Rules.MaxAttempts = 0 }},
		{"unknown edit scope", func(in *Input) { in.Rules.EditScope = "everything" }},
		{"empty edit scope", func(in *Input) { in.Rules.EditScope = "" }},
		{"empty forbidden pattern", func(in *Input) { in.Rules.ForbiddenPatterns = []string{""} }},
		{"negative attempts used", func(in *Input) { in.AttemptsUsed = -1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := Input{Diff: protectedDiff, Rules: DefaultRules()}
			tt.edit(&in)
			v := Evaluate(in)
			if v.Allowed || len(v.Reasons) != 1 {
				t.Fatalf("verdict = %+v, want one reason", v)
			}
			if v.Reasons[0].Rule != RuleInvalidRules {
				t.Errorf("rule = %q, want %q", v.Reasons[0].Rule, RuleInvalidRules)
			}
		})
	}

	t.Run("one reason per problem", func(t *testing.T) {
		in := Input{Diff: protectedDiff, Rules: Rules{EditScope: "x", ForbiddenPatterns: []string{""}}, AttemptsUsed: -1}
		v := Evaluate(in)
		// MaxChangedLines, MaxDeletedLines is 0 (valid), MaxAttempts, scope, pattern, attempts.
		if got := len(v.Reasons); got != 5 {
			t.Fatalf("got %d reasons: %+v", got, v.Reasons)
		}
		for _, r := range v.Reasons {
			if r.Rule != RuleInvalidRules {
				t.Errorf("rule = %q", r.Rule)
			}
		}
	})
}

func TestEvaluateInvalidPath(t *testing.T) {
	bad := []struct{ name, path string }{
		{"empty", ""},
		{"leading slash", "/etc/passwd"},
		{"dot dot in middle", "a/../b"},
		{"parent traversal", "../x"},
		{"bare dot dot", ".."},
		{"backslash", `src\a.ts`},
		{"NUL byte", "src/a\x00.ts"},
		{"not clean", "src//a.ts"},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			in := Input{
				Diff:       Diff{Files: []FileChange{{Path: tt.path, Binary: true, Special: "symlink", Added: []Line{{Number: 1, Text: "eslint-disable"}}}}},
				Rules:      rulesWith(func(r *Rules) { r.ForbiddenPatterns = []string{"eslint-disable"} }),
				ErrorFiles: []string{"src/a.ts"},
			}
			v := Evaluate(in)
			if v.Allowed {
				t.Fatal("allowed")
			}
			if got := ruleNames(v); !slices.Equal(got, []string{RuleInvalidPath}) {
				t.Errorf("rules = %v, want only invalid_path (no other per-file rule)", got)
			}
		})
	}

	t.Run("valid file in the same diff is still evaluated", func(t *testing.T) {
		in := Input{
			Diff: Diff{Files: []FileChange{
				mod("../x", 0, "a"),
				mod("src/b.ts", 0, "b"),
			}},
			Rules:      DefaultRules(),
			ErrorFiles: []string{"src/a.ts"},
		}
		v := Evaluate(in)
		want := []string{RuleInvalidPath, RuleEditScope}
		if got := ruleNames(v); !slices.Equal(got, want) {
			t.Fatalf("rules = %v, want %v", got, want)
		}
		if v.Reasons[1].File != "src/b.ts" {
			t.Errorf("File = %q", v.Reasons[1].File)
		}
	})

	t.Run("totals still count the invalid file's lines", func(t *testing.T) {
		in := Input{
			Diff:  Diff{Files: []FileChange{mod("../x", 51)}},
			Rules: rulesWith(func(r *Rules) { r.EditScope = ScopeAny }),
		}
		want := []string{RuleInvalidPath, RuleMaxDeletedLines}
		if got := ruleNames(Evaluate(in)); !slices.Equal(got, want) {
			t.Fatalf("rules = %v, want %v", got, want)
		}
	})
}

func TestEvaluateIsPureAndDeterministic(t *testing.T) {
	in := Input{
		Diff: Diff{Files: []FileChange{
			mod("regreen.yml", 60, "// eslint-disable"),
			mod("src/b.ts", 3, "x", "y"),
		}},
		Rules: Rules{
			ProtectedFiles: []string{"*.lock"}, ForbiddenPatterns: []string{"eslint-disable"},
			MaxChangedLines: 10, MaxDeletedLines: 5, MaxAttempts: 2, EditScope: ScopeFilesInErrors,
		},
		ErrorFiles:   []string{"./src/a.ts", "src/c.ts"},
		AttemptsUsed: 1,
	}
	snapshot := deepCopyInput(in)

	first := Evaluate(in)
	second := Evaluate(in)
	if !reflect.DeepEqual(first, second) {
		t.Errorf("verdicts differ:\n%+v\n%+v", first, second)
	}
	if !reflect.DeepEqual(in, snapshot) {
		t.Errorf("input was mutated:\n%+v\n%+v", in, snapshot)
	}
}

func deepCopyInput(in Input) Input {
	out := in
	out.Diff.Files = make([]FileChange, len(in.Diff.Files))
	for i, f := range in.Diff.Files {
		f.Added = slices.Clone(f.Added)
		out.Diff.Files[i] = f
	}
	out.Rules.ProtectedFiles = slices.Clone(in.Rules.ProtectedFiles)
	out.Rules.ForbiddenPatterns = slices.Clone(in.Rules.ForbiddenPatterns)
	out.ErrorFiles = slices.Clone(in.ErrorFiles)
	return out
}

func TestEvaluateMessagesAreOneSentence(t *testing.T) {
	in := Input{
		Diff: Diff{Files: []FileChange{
			mod("regreen.yml", 60, "eslint-disable"),
			{Path: "b.bin", Binary: true},
			{Path: "l", Special: "symlink"},
		}},
		Rules: rulesWith(func(r *Rules) { r.ForbiddenPatterns = []string{"eslint-disable"} }),
	}
	for _, r := range Evaluate(in).Reasons {
		if r.Message == "" || strings.Contains(r.Message, "\n") {
			t.Errorf("bad message for %s: %q", r.Rule, r.Message)
		}
		if r.Rule == "" {
			t.Errorf("empty rule: %+v", r)
		}
	}
}
