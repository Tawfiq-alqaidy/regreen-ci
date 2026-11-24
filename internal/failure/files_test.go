package failure

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Tawfiq-alqaidy/regreen-ci/internal/ci"
)

func knownSet() map[string]struct{} {
	m := map[string]struct{}{}
	for _, f := range []string{
		"src/a.ts", "src/b.ts", "main.go", "internal/x/y.go",
		"docs/my file.md", "pages/[id].tsx", "app/(auth)/page.tsx", "src/a=b.ts",
	} {
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
		{"whitespace in name", "docs/my file.md", 0, []string{"docs/my file.md"}},
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

// delimiterNames are known paths whose characters split first-pass tokens.
var delimiterNames = []string{"pages/[id].tsx", "app/(auth)/page.tsx", "src/a=b.ts", "docs/my file.md"}

func TestExtractFilesDelimiterNames(t *testing.T) {
	forms := []struct {
		name  string
		build func(n string) string
		found bool
	}{
		{"plain", func(n string) string { return n }, true},
		{"position", func(n string) string { return n + ":12:5 - error" }, true},
		{"paren position", func(n string) string { return n + "(12,5): error" }, true},
		{"double quoted", func(n string) string { return `see "` + n + `" here` }, true},
		{"single quoted", func(n string) string { return "see '" + n + "'" }, true},
		{"backquoted", func(n string) string { return "see `" + n + "`" }, true},
		{"after equals", func(n string) string { return "file=" + n + ";" }, true},
		{"in parens", func(n string) string { return "(" + n + ")" }, true},
		{"after comma", func(n string) string { return "a.ts, " + n + ", b.ts" }, true},
		{"absolute runner path", func(n string) string { return "/home/runner/work/app/app/" + n + ":3:1" }, true},
		{"absolute windows path", func(n string) string {
			return `D:\a\app\app\` + strings.ReplaceAll(n, "/", `\`) + "(3,1): error"
		}, true},
		{"node stack frame", func(n string) string { return "    at f (/home/runner/work/app/app/" + n + ":3:5)" }, true},
		{"dot slash prefix", func(n string) string { return "./" + n + ":1" }, true},
		{"second line", func(n string) string { return "first line\n" + n }, true},
		{"relative prefix", func(n string) string { return "x/" + n + ":1" }, false},
		{"relative prefix of an absolute-looking token", func(n string) string { return "a/home/" + n }, false},
		{"dotdot in absolute path", func(n string) string { return "/etc/../" + n }, false},
		{"dotdot relative", func(n string) string { return "../" + n }, false},
		{"glued to a word", func(n string) string { return "x" + n }, false},
		{"followed by a letter", func(n string) string { return n + "x" }, false},
		{"followed by a dot", func(n string) string { return n + ".bak" }, false},
		{"before a line cut", func(n string) string { return "see " + n + lineCutMarker }, false},
		{"split over two lines", func(n string) string { return n[:3] + "\n" + n[3:] }, false},
	}
	for _, n := range delimiterNames {
		for _, form := range forms {
			t.Run(n+"/"+form.name, func(t *testing.T) {
				text := form.build(n)
				got := ExtractFiles(text, knownSet(), 0)
				var want []string
				if form.found {
					want = []string{n}
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("ExtractFiles(%q) = %q, want %q", text, got, want)
				}
			})
		}
	}
}

func TestExtractFilesDelimiterNameBoundaries(t *testing.T) {
	known := map[string]struct{}{"pages/[id].tsx": {}, "pages/[id].tsx.bak": {}, "a b": {}, "a b c": {}}
	tests := []struct {
		name string
		text string
		want []string
	}{
		{"longer name only", "pages/[id].tsx.bak:1", []string{"pages/[id].tsx.bak"}},
		{"shorter name only", "pages/[id].tsx:1", []string{"pages/[id].tsx"}},
		{"both", "pages/[id].tsx.bak pages/[id].tsx", []string{"pages/[id].tsx", "pages/[id].tsx.bak"}},
		// "a b" ends at a space inside "a b c": both are named.
		{"prefix ending at a space", "a b c", []string{"a b", "a b c"}},
		{"cut right after a name", "x a b" + lineCutMarker, nil},
		{"cut after a separator", "x a b " + lineCutMarker, []string{"a b"}},
		{"cut line, earlier occurrence", "a b, a b" + lineCutMarker, []string{"a b"}},
		{"overlapping occurrences", "a ba b", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExtractFiles(tc.text, known, 0); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ExtractFiles(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

func TestExtractFilesDelimiterNameLimit(t *testing.T) {
	mk := func(n int) map[string]struct{} {
		m := map[string]struct{}{}
		for i := range n {
			m[fmt.Sprintf("dir %04d/file.ts", i)] = struct{}{}
		}
		return m
	}
	text := strings.Repeat("noise line\n", 1000) + "dir 0007/file.ts:1"
	if got := ExtractFiles(text, mk(maxSpecialPaths), 0); !reflect.DeepEqual(got, []string{"dir 0007/file.ts"}) {
		t.Fatalf("at the limit: got %q", got)
	}
	if got := ExtractFiles(text, mk(maxSpecialPaths+1), 0); got != nil {
		t.Fatalf("over the limit the second pass must be skipped: got %q", got)
	}
}

// TestExtractFilesDelimiterNamesAreLinear would take minutes with a naive
// search: the name is long and periodic, and the text repeats its prefix.
func TestExtractFilesDelimiterNamesAreLinear(t *testing.T) {
	name := strings.Repeat("a ", 2000) + "b"
	known := map[string]struct{}{name: {}}
	text := strings.Repeat("a ", 1<<20) + "b"
	if got := ExtractFiles(text, known, 0); !reflect.DeepEqual(got, []string{name}) {
		t.Fatalf("got %d files", len(got))
	}
}

// TestExtractFilesNoAbsoluteAfterClosingDelimiter covers relative paths whose
// directories hold brackets: the part after "]" or ")" is not an absolute
// path, so it must not suffix-match a known file.
func TestExtractFilesNoAbsoluteAfterClosingDelimiter(t *testing.T) {
	known := map[string]struct{}{
		"pages/[id].tsx": {}, "blog/[id]/page.tsx": {}, "(auth)/page.tsx": {}, "src/a.ts": {},
	}
	tests := []struct {
		name string
		text string
		want []string
	}{
		{"second pass, bracket dir", "foo/[x]/pages/[id].tsx", nil},
		{"second pass, locale dir", "app/[locale]/blog/[id]/page.tsx", nil},
		{"second pass, route group", "app/(shop)/blog/[id]/page.tsx", nil},
		{"second pass, odd dir", "rel/dir/x]/(auth)/page.tsx", nil},
		{"second pass, stack frame", "    at f (/home/runner/work/app/app/pages/[id].tsx:3:5)", []string{"pages/[id].tsx"}},
		{"first pass, locale dir", "app/[locale]/src/a.ts", nil},
		{"first pass, after paren", "x)/src/a.ts", nil},
		{"first pass, after brace", "a}/src/a.ts", nil},
		{"first pass, after angle", "b>/src/a.ts", nil},
		{"first pass, flag value", "--config=/home/runner/work/app/app/src/a.ts", []string{"src/a.ts"}},
		{"first pass, in parens", "(/home/runner/work/app/app/src/a.ts)", []string{"src/a.ts"}},
		{"first pass, quoted", `"/home/runner/work/app/app/src/a.ts"`, []string{"src/a.ts"}},
		{"first pass, comma list", "/r/x/src/a.ts,/r/y/pages/[id].tsx", []string{"pages/[id].tsx", "src/a.ts"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExtractFiles(tc.text, known, 0); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ExtractFiles(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

func TestExtractFilesSecondPassBudget(t *testing.T) {
	known := map[string]struct{}{"src/a.ts": {}}
	for i := range maxSpecialPaths {
		known[fmt.Sprintf("dir %04d/file.ts", i)] = struct{}{}
	}
	// 16 KiB (TrimLog's default cap) with 2000 such paths is in budget.
	small := "dir 0007/file.ts src/a.ts\n" + strings.Repeat("x", 16<<10-27)
	if got := ExtractFiles(small, known, 0); !reflect.DeepEqual(got, []string{"dir 0007/file.ts", "src/a.ts"}) {
		t.Fatalf("in budget: got %q", got)
	}
	// Past the budget only the first pass runs.
	big := small + strings.Repeat("y", secondPassBudget/maxSpecialPaths)
	if got := ExtractFiles(big, known, 0); !reflect.DeepEqual(got, []string{"src/a.ts"}) {
		t.Fatalf("over budget: got %q", got)
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

func TestExtractFilesIgnoresFragmentBeforeLineCut(t *testing.T) {
	known := map[string]struct{}{"config.js": {}, "config.json": {}}
	long := strings.Repeat("x", 1990) + " config.json"
	text, trunc := TrimLog(long, Limits{})
	if !trunc || !strings.HasSuffix(text, lineCutMarker) {
		t.Fatalf("setup: expected a cut line, got truncated=%v", trunc)
	}
	if got := ExtractFiles(text, known, 0); got != nil {
		t.Fatalf("fragment matched: %v", got)
	}
	// A complete token before the fragment still counts, and so does one
	// on an uncut line.
	known["a.go"] = struct{}{}
	got := ExtractFiles("a.go "+text+"\nconfig.js", known, 0)
	if !reflect.DeepEqual(got, []string{"a.go", "config.js"}) {
		t.Fatalf("got %v", got)
	}
	// Without the marker's leading space it is plain text, not a cut.
	if got := ExtractFiles("config.js"+lineCutMarker[1:], known, 0); got != nil {
		t.Fatalf("token glued to marker text is not a known file: %v", got)
	}
	if got := ExtractFiles("config.js\t"+lineCutMarker[1:], known, 0); !reflect.DeepEqual(got, []string{"config.js"}) {
		t.Fatalf("marker text without its leading space is plain content: %v", got)
	}
	if got := ExtractFiles("config.js "+lineCutMarker, known, 0); !reflect.DeepEqual(got, []string{"config.js"}) {
		t.Fatalf("complete token before a cut: %v", got)
	}
}

func TestExtractFilesSlashRunsAreCheap(t *testing.T) {
	tok := strings.Repeat("/", 4000) + "src/a.ts"
	text := strings.Repeat(tok+" "+strings.Repeat("a:1", 1300)+" ", 10<<20/(len(tok)+4000))
	if got := ExtractFiles(text, knownSet(), 0); !reflect.DeepEqual(got, []string{"src/a.ts"}) {
		t.Fatalf("got %v", got)
	}
}

func TestExtractFilesSuffixLimitedByLongestKnown(t *testing.T) {
	known := map[string]struct{}{"a/b.go": {}}
	if got := ExtractFiles("/r/w/a/b.go", known, 0); !reflect.DeepEqual(got, []string{"a/b.go"}) {
		t.Fatalf("got %v", got)
	}
}

func TestNoMidLineCutWithoutMarker(t *testing.T) {
	known := map[string]struct{}{"config.js": {}, "config.json": {}}
	tests := []struct {
		name string
		log  string
		lim  Limits
	}{
		{"small byte cap", "foo config.json", Limits{MaxBytes: 13}},
		{"long line, byte cap", strings.Repeat("x", 1990) + " config.json", Limits{MaxBytes: 2000}},
		{"long line, default", strings.Repeat("x", 1990) + " config.json", Limits{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Build(ci.StepLog{Commit: "c", Log: tc.log}, nil, known, tc.lim)
			if len(got.Files) != 0 {
				t.Fatalf("Files = %v from %q", got.Files, got.Errors)
			}
		})
	}
}
