package failure

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Tawfiq-alqaidy/regreen-ci/internal/ci"
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
