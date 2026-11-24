package failure

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const lineCutMarker = " …[line cut]"

// TrimLog sanitizes and caps untrusted text, keeping the tail. It replaces
// invalid UTF-8, normalizes line endings, removes terminal escape sequences,
// control characters and bidirectional overrides, then enforces the limits.
// Non-positive limit fields take their DefaultLimits value. The returned
// text never ends with a newline; truncated reports that anything was cut.
func TrimLog(raw string, lim Limits) (text string, truncated bool) {
	lim = withDefaults(lim)
	lines := splitLines(sanitize(raw))
	lines = tidyLines(lines)
	if len(lines) == 0 {
		return "", false
	}
	for i, l := range lines {
		if len(l) > lim.MaxLineBytes {
			lines[i] = cutRunes(l, lim.MaxLineBytes) + lineCutMarker
			truncated = true
		}
	}
	keep := tailThatFits(lines, lim)
	if keep < len(lines) {
		truncated = true
	}
	tail := lines[len(lines)-keep:]
	if len(tail) == 1 && len(tail[0]) > lim.MaxBytes {
		tail[0] = cutRunes(tail[0], lim.MaxBytes)
		truncated = true
	}
	out := strings.Join(tail, "\n")
	if dropped := len(lines) - keep; dropped > 0 {
		out = fmt.Sprintf("[… %d earlier lines trimmed …]\n%s", dropped, out)
	}
	return out, truncated
}

func withDefaults(lim Limits) Limits {
	d := DefaultLimits()
	if lim.MaxLines <= 0 {
		lim.MaxLines = d.MaxLines
	}
	if lim.MaxBytes <= 0 {
		lim.MaxBytes = d.MaxBytes
	}
	if lim.MaxLineBytes <= 0 {
		lim.MaxLineBytes = d.MaxLineBytes
	}
	if lim.MaxFiles <= 0 {
		lim.MaxFiles = d.MaxFiles
	}
	return lim
}

// sanitize applies rules 1-3: valid UTF-8, \n line endings, and no escape
// sequences, control characters or bidirectional overrides.
func sanitize(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	var b strings.Builder
	b.Grow(len(raw))
	for i := 0; i < len(raw); {
		r, size := utf8.DecodeRuneInString(raw[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			b.WriteRune(unicode.ReplacementChar)
			i++
		case r == 0x1b:
			i += skipEscape(raw[i+1:]) + 1
		case r == '\n' || r == '\t':
			b.WriteRune(r)
			i += size
		case unicode.IsControl(r), isBidiControl(r):
			i += size
		default:
			b.WriteString(raw[i : i+size])
			i += size
		}
	}
	return b.String()
}

// skipEscape returns how many bytes of s (the text after an ESC) belong to
// the escape sequence. A CSI or OSC sequence that is not terminated before
// the end of the line ends at the newline, which is kept, so an unterminated
// sequence cannot swallow the rest of the log. A lone ESC consumes nothing.
func skipEscape(s string) int {
	if s == "" {
		return 0
	}
	switch s[0] {
	case '[':
		for i := 1; i < len(s); i++ {
			switch {
			case s[i] == '\n':
				return i
			case s[i] >= '@' && s[i] <= '~':
				return i + 1
			}
		}
		return len(s)
	case ']':
		for i := 1; i < len(s); i++ {
			switch {
			case s[i] == '\n':
				return i
			case s[i] == '\a':
				return i + 1
			case s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\':
				return i + 2
			}
		}
		return len(s)
	}
	return 0
}

func isBidiControl(r rune) bool {
	return (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069)
}

func splitLines(s string) []string { return strings.Split(s, "\n") }

// tidyLines applies rule 4: trailing blanks trimmed, runs of three or more
// blank lines collapsed to two, leading and trailing blank lines dropped.
func tidyLines(in []string) []string {
	out := make([]string, 0, len(in))
	blanks := 0
	for _, l := range in {
		l = strings.TrimRight(l, " \t")
		if l == "" {
			blanks++
			if len(out) == 0 || blanks > 2 {
				continue
			}
		} else {
			blanks = 0
		}
		out = append(out, l)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// cutRunes returns the longest prefix of s of at most n bytes that ends on a
// rune boundary.
func cutRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// tailThatFits returns how many trailing lines fit both caps. It is at least
// one: the last line is always kept.
func tailThatFits(lines []string, lim Limits) int {
	keep, size := 0, 0
	for i := len(lines) - 1; i >= 0; i-- {
		add := len(lines[i])
		if keep > 0 {
			add++ // newline joining it to the line after
		}
		if keep >= lim.MaxLines || (keep > 0 && size+add > lim.MaxBytes) {
			break
		}
		keep++
		size += add
	}
	return keep
}
