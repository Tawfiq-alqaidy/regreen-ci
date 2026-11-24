package failure

import (
	"reflect"
	"strings"
	"testing"
)

func knownSet() map[string]struct{} {
	m := map[string]struct{}{}
	for _, f := range []string{"src/a.ts", "src/b.ts", "main.go", "internal/x/y.go", "docs/my file.md"} {
		m[f] = struct{}{}
	}
	return m
}

func TestExtractFiles(t *testing.T) {
	tests := []struct {
		name string
		text string
		max  int
		want []string
	}{
		{"position suffix", "src/a.ts:12:5 - error TS2322", 0, []string{"src/a.ts"}},
		{"paren position", "./src/a.ts(12,5): error", 0, []string{"src/a.ts"}},
		{"absolute posix", "/home/runner/work/app/app/src/b.ts:3", 0, []string{"src/b.ts"}},
		{"absolute windows", `D:\a\app\app\src\b.ts:3:1`, 0, []string{"src/b.ts"}},
		{"two files sorted", "main.go:10:2: unused variable\ninternal/x/y.go:1: x", 0, []string{"internal/x/y.go", "main.go"}},
		{"quotes and parens", `see "src/a.ts", and (src/b.ts).`, 0, []string{"src/a.ts", "src/b.ts"}},
		{"relative must match exactly", "x/src/a.ts:1", 0, nil},
		{"dotdot relative", "../src/a.ts", 0, nil},
		{"dotdot absolute", "/etc/../src/a.ts", 0, nil},
		{"unknown file", "src/c.ts:1", 0, nil},
		{"whitespace in name", "docs/my file.md", 0, nil},
		{"repeated once", "src/a.ts src/a.ts:1 ./src/a.ts", 0, []string{"src/a.ts"}},
		{"max one", "src/b.ts src/a.ts", 1, []string{"src/a.ts"}},
		{"repeated dot slash", "././src/a.ts", 0, []string{"src/a.ts"}},
		{"trailing dot and colon", "src/a.ts.: src/b.ts:", 0, []string{"src/a.ts", "src/b.ts"}},
		{"equals and pipes", "file=src/a.ts|src/b.ts;", 0, []string{"src/a.ts", "src/b.ts"}},
		{"drive without slash is relative", "C:src/a.ts", 0, nil},
		{"empty text", "", 0, nil},
		{"only punctuation", ": . ./ ,", 0, nil},
		{"token over 4096 bytes", strings.Repeat("a", 5000) + "/src/a.ts", 0, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractFiles(tc.text, knownSet(), tc.max)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ExtractFiles(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestExtractFilesNoKnown(t *testing.T) {
	if got := ExtractFiles("src/a.ts", nil, 0); got != nil {
		t.Fatalf("nil known: got %v", got)
	}
	if got := ExtractFiles("src/a.ts", map[string]struct{}{}, 0); got != nil {
		t.Fatalf("empty known: got %v", got)
	}
}

func TestExtractFilesDefaultMax(t *testing.T) {
	known := map[string]struct{}{}
	var text strings.Builder
	for i := 0; i < 150; i++ {
		name := "f" + strings.Repeat("x", i%7) + string(rune('a'+i/26)) + string(rune('a'+i%26)) + ".go"
		known[name] = struct{}{}
		text.WriteString(name + "\n")
	}
	got := ExtractFiles(text.String(), known, 0)
	if len(got) != DefaultLimits().MaxFiles {
		t.Fatalf("len = %d, want %d", len(got), DefaultLimits().MaxFiles)
	}
}
