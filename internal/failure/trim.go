package failure

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// lineCutMarker is appended to a line cut by MaxLineBytes. It is 14 bytes.
const lineCutMarker = " …[line cut]"

// TrimLog sanitizes and caps untrusted text, keeping the tail. It replaces
// invalid UTF-8, normalizes line endings, removes terminal escape sequences,
// control characters and bidirectional overrides, then enforces the limits.
// Non-positive limit fields take their DefaultLimits value, and MaxBytes is
// raised to the smallest useful cap (the cut marker plus one rune). A line is
// cut to MaxLineBytes (or less, so that it plus the marker fits MaxBytes) and
// always carries the marker; nothing is ever cut inside a line without it.
// When earlier lines are dropped, blank lines at the start of the kept tail
// go too, and a header line counts them all. The returned text never ends
// with a newline; truncated reports that anything was cut.
func TrimLog(raw string, lim Limits) (text string, truncated bool) {
	tail, dropped, truncated := trimLines(raw, lim)
	out := strings.Join(tail, "\n")
	if dropped > 0 {
		out = fmt.Sprintf("[… %d earlier lines trimmed …]\n%s", dropped, out)
	}
	return out, truncated
}

// trimLines does TrimLog's work. It returns the kept lines and how many
// earlier lines were dropped.
func trimLines(raw string, lim Limits) (tail []string, dropped int, truncated bool) {
	lim = withDefaults(lim)
	lines := splitLines(sanitize(raw))
	lines = tidyLines(lines)
	if len(lines) == 0 {
		return nil, 0, false
	}
	// A cut line (content plus marker) must fit in MaxBytes, so that every
	// cut inside a line is a marked one and the last line is always kept
	// whole. withDefaults guarantees MaxBytes leaves room for the marker.
	maxLine := min(lim.MaxLineBytes, lim.MaxBytes-len(lineCutMarker))
	for i, l := range lines {
		if len(l) <= maxLine {
			continue
		}
		truncated = true
		// A line cut by an earlier pass keeps its marker as it is, so the
		// marker never turns into plain content.
		if content, ok := strings.CutSuffix(l, lineCutMarker); ok && len(content) <= maxLine {
			continue
		}
		lines[i] = cutRunes(l, maxLine) + lineCutMarker
	}
	keep := tailThatFits(lines, lim)
	tail = lines[len(lines)-keep:]
	dropped = len(lines) - keep
	if dropped == 0 {
		return tail, 0, truncated
	}
	// The last line is never blank (tidyLines), so this stops.
	for tail[0] == "" {
		tail = tail[1:]
		dropped++
	}
	return tail, dropped, true
}

func withDefaults(lim Limits) Limits {
	d := DefaultLimits()
	if lim.MaxLines <= 0 {
		lim.MaxLines = d.MaxLines
	}
	if lim.MaxBytes <= 0 {
		lim.MaxBytes = d.MaxBytes
	}
	// The smallest useful cap holds a cut line: the marker plus one rune.
	lim.MaxBytes = max(lim.MaxBytes, len(lineCutMarker)+utf8.UTFMax)
	if lim.MaxLineBytes <= 0 {
		lim.MaxLineBytes = d.MaxLineBytes
	}
	if lim.MaxFiles <= 0 {
		lim.MaxFiles = d.MaxFiles
	}
	return lim
}

// sanitize applies rules 1-3: valid UTF-8, \n line endings, and no escape
// sequences, control characters or invisible format characters (category Cf,
// which includes the bidirectional overrides, zero-width characters and tags).
func sanitize(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	// Unicode line separators would forge line breaks in text shown to a
	// reader or agent; make them real ones so the line limits count them.
	raw = strings.NewReplacer("\u2028", "\n", "\u2029", "\n", "\u0085", "\n").Replace(raw)
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
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
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
// one, and every line fits MaxBytes on its own after the line cut.
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
