package failure

import (
	"fmt"
	"strings"
	"testing"
)

type trimCase struct {
	name      string
	in        string
	lim       Limits
	want      string
	truncated bool
}

func trimCases() []trimCase {
	many := make([]string, 500)
	for i := range many {
		many[i] = fmt.Sprintf("l%d", i)
	}
	manyIn := strings.Join(many, "\n")
	tail := strings.Join(many[490:], "\n")
	// "é" is two bytes; with MaxLineBytes 5 the cut falls inside it.
	return []trimCase{
		{name: "plain", in: "hello\nworld", want: "hello\nworld"},
		{name: "crlf", in: "a\r\nb\rc\n", want: "a\nb\nc"},
		{name: "ansi csi", in: "\x1b[31merror\x1b[0m", want: "error"},
		{name: "ansi csi params", in: "\x1b[1;38;5;196mbad\x1b[K", want: "bad"},
		{name: "osc bel", in: "\x1b]8;;http://x.example\aclick\x1b]8;;\a", want: "click"},
		{name: "osc st", in: "\x1b]0;title\x1b\\text", want: "text"},
		{name: "unterminated osc stops at newline", in: "a\x1b]0;title\nb", want: "a\nb"},
		{name: "unterminated csi stops at newline", in: "a\x1b[12\nb", want: "a\nb"},
		{name: "lone esc", in: "a\x1bb", want: "ab"},
		{name: "trailing esc", in: "a\x1b", want: "a"},
		{name: "nul bel del", in: "a\x00b\x07c\x7fd", want: "abcd"},
		{name: "c1 controls", in: "a\u0085b\u009bc", want: "abc"},
		{name: "bidi overrides", in: "a\u202eb\u202ac\u2066d\u2069e", want: "abcde"},
		{name: "tab kept", in: "a\tb", want: "a\tb"},
		{name: "invalid utf8", in: "a\xffb", want: "a\uFFFDb"},
		{name: "four blank lines", in: "a\n\n\n\n\nb", want: "a\n\n\nb"},
		{name: "three blank lines", in: "a\n\n\n\nb", want: "a\n\n\nb"},
		{name: "two blank lines kept", in: "a\n\n\nb", want: "a\n\n\nb"},
		{name: "one blank line kept", in: "a\n\nb", want: "a\n\nb"},
		{name: "whitespace-only lines are blank", in: "a\n \t\n\n  \nb", want: "a\n\n\nb"},
		{name: "trailing spaces trimmed", in: "a \t\nb  ", want: "a\nb"},
		{name: "leading and trailing blanks dropped", in: "\n\n a\n\n\n", want: " a"},
		{name: "empty", in: "", want: ""},
		{name: "all blank", in: " \n\t\n\r\n", want: ""},
		{
			name: "long line cut at rune boundary", in: "abcdéfg", lim: Limits{MaxLineBytes: 5},
			want: "abcd …[line cut]", truncated: true,
		},
		{
			name: "long line cut, rune fits exactly", in: "abcéfg", lim: Limits{MaxLineBytes: 5},
			want: "abcé …[line cut]", truncated: true,
		},
		{name: "line at limit untouched", in: "abcde", lim: Limits{MaxLineBytes: 5}, want: "abcde"},
		{
			name: "line cap", in: manyIn, lim: Limits{MaxLines: 10},
			want: "[… 490 earlier lines trimmed …]\n" + tail, truncated: true,
		},
		{
			name: "byte cap smaller than line cap", in: "aaaa\nbbbb\ncccc\ndddd", lim: Limits{MaxBytes: 10},
			want: "[… 2 earlier lines trimmed …]\ncccc\ndddd", truncated: true,
		},
		{name: "byte cap exact fit", in: "aaaa\nbbbb", lim: Limits{MaxBytes: 9}, want: "aaaa\nbbbb"},
		{
			name: "single last line over byte cap", in: "x\nabcdéfgh", lim: Limits{MaxBytes: 5, MaxLineBytes: 100},
			want: "[… 1 earlier lines trimmed …]\nabcd", truncated: true,
		},
		{
			name: "byte cap smaller than one rune", in: "0\n\xff", lim: Limits{MaxBytes: 1},
			want: "[… 1 earlier lines trimmed …]\n\uFFFD", truncated: true,
		},
		{name: "zero limits behave like defaults", in: "a\nb", lim: Limits{}, want: "a\nb"},
		{name: "marker not counted", in: "a\nb\nc", lim: Limits{MaxLines: 2}, want: "[… 1 earlier lines trimmed …]\nb\nc", truncated: true},
	}
}

func TestTrimLog(t *testing.T) {
	for _, tc := range trimCases() {
		t.Run(tc.name, func(t *testing.T) {
			got, trunc := TrimLog(tc.in, tc.lim)
			if got != tc.want || trunc != tc.truncated {
				t.Fatalf("TrimLog(%q) = (%q, %v), want (%q, %v)", tc.in, got, trunc, tc.want, tc.truncated)
			}
		})
	}
}

func TestTrimLogIdempotent(t *testing.T) {
	for _, tc := range trimCases() {
		if tc.truncated {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			once, _ := TrimLog(tc.in, tc.lim)
			twice, trunc := TrimLog(once, tc.lim)
			if twice != once || trunc {
				t.Fatalf("not idempotent: %q -> %q (truncated %v)", once, twice, trunc)
			}
		})
	}
}

func TestTrimLogDefaultsApplyPerField(t *testing.T) {
	d := DefaultLimits()
	if d != (Limits{MaxLines: 200, MaxBytes: 16384, MaxLineBytes: 2000, MaxFiles: 100}) {
		t.Fatalf("DefaultLimits = %+v", d)
	}
	long := strings.Repeat("x", d.MaxLineBytes+10)
	got, trunc := TrimLog(long, Limits{MaxLines: 5})
	if !trunc || !strings.HasSuffix(got, " …[line cut]") || len(got) != d.MaxLineBytes+len(" …[line cut]") {
		t.Fatalf("default MaxLineBytes not applied: len %d trunc %v", len(got), trunc)
	}
	big := strings.Repeat("0123456789\n", 3000)
	got, trunc = TrimLog(big, Limits{})
	if !trunc || len(got) > d.MaxBytes+len("[… 99999 earlier lines trimmed …]\n") || strings.Count(got, "\n") > d.MaxLines {
		t.Fatalf("default caps not applied: len %d lines %d", len(got), strings.Count(got, "\n"))
	}
}

func TestTrimLogLineLimitLargerThanByteLimit(t *testing.T) {
	got, trunc := TrimLog("héllo wörld", Limits{MaxBytes: 4, MaxLineBytes: 100})
	if got != "hél" || !trunc {
		t.Fatalf("got (%q, %v)", got, trunc)
	}
}
