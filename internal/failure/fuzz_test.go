package failure

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func FuzzTrimLog(f *testing.F) {
	for _, tc := range trimCases() {
		f.Add(tc.in, tc.lim.MaxLines, tc.lim.MaxBytes, tc.lim.MaxLineBytes)
	}
	f.Add("\x1b]0;x\x1b", 3, 10, 5)
	f.Fuzz(func(t *testing.T, in string, maxLines, maxBytes, maxLineBytes int) {
		lim := Limits{MaxLines: maxLines % 50, MaxBytes: maxBytes % 500, MaxLineBytes: maxLineBytes % 200}
		got, truncated := TrimLog(in, lim)
		eff := withDefaults(lim)
		if !utf8.ValidString(got) {
			t.Fatalf("invalid UTF-8: %q", got)
		}
		if strings.ContainsRune(got, 0x1b) {
			t.Fatalf("ESC in output: %q", got)
		}
		for _, r := range got {
			if (unicode.IsControl(r) && r != '\n' && r != '\t') || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
				t.Fatalf("forbidden rune %U in %q", r, got)
			}
		}
		if n := strings.Count(got, "\n") + 1; got != "" && n > eff.MaxLines+1 {
			t.Fatalf("%d lines exceeds MaxLines+1 (%d)", n, eff.MaxLines+1)
		}
		if strings.HasSuffix(got, "\n") {
			t.Fatalf("trailing newline: %q", got)
		}
		// The kept tail is the output minus the header line, which is there
		// exactly when earlier lines were dropped. It fits MaxBytes.
		tail := got
		if _, dropped, _ := trimLines(in, lim); dropped > 0 {
			var header string
			header, tail, _ = strings.Cut(got, "\n")
			if want := fmt.Sprintf("[… %d earlier lines trimmed …]", dropped); header != want || !truncated {
				t.Fatalf("header %q, truncated %v; want %q, true", header, truncated, want)
			}
		}
		if len(tail) > eff.MaxBytes {
			t.Fatalf("tail is %d bytes, cap %d: %q", len(tail), eff.MaxBytes, got)
		}
		// Re-trimming the tail returns it unchanged.
		if again, _ := TrimLog(tail, lim); again != tail {
			t.Fatalf("not idempotent: %q -> %q", tail, again)
		}
	})
}

func FuzzExtractFiles(f *testing.F) {
	for _, s := range []string{
		"src/a.ts:12:5 - error TS2322", "./src/a.ts(12,5): error", "/home/runner/work/app/app/src/b.ts:3",
		`D:\a\app\app\src\b.ts:3:1`, "main.go:10:2: unused variable\ninternal/x/y.go:1: x",
		`see "src/a.ts", and (src/b.ts).`, "x/src/a.ts:1", "../src/a.ts", "/etc/../src/a.ts", "docs/my file.md",
		"/home/runner/work/app/app/pages/[id].tsx:3:5", `"app/(auth)/page.tsx"`, "src/a=b.ts:1", "x/pages/[id].tsx",
		"pages/[id].tsx" + lineCutMarker, `D:\a\app\app\docs\my file.md(1,1)`,
	} {
		f.Add(s, 0)
		f.Add(s, 1)
	}
	known := knownSet()
	f.Fuzz(func(t *testing.T, text string, maxFiles int) {
		got := ExtractFiles(text, known, maxFiles%5)
		if !sort.StringsAreSorted(got) {
			t.Fatalf("not sorted: %v", got)
		}
		for i, g := range got {
			if _, ok := known[g]; !ok {
				t.Fatalf("%q not in known", g)
			}
			if i > 0 && got[i-1] == g {
				t.Fatalf("duplicate %q", g)
			}
		}
		if len(slices.Compact(slices.Clone(got))) != len(got) {
			t.Fatalf("duplicates in %v", got)
		}
	})
}
