package github

import (
	"strings"
	"time"
)

const byteOrderMark = "\xef\xbb\xbf"

// splitTimestamp returns the line's leading RFC 3339 timestamp and the text
// after it (without the one separating space). ok is false when the first
// space-delimited field is not a timestamp.
func splitTimestamp(line string) (t time.Time, rest string, ok bool) {
	field, rest, _ := strings.Cut(line, " ")
	t, err := time.Parse(time.RFC3339Nano, field)
	if err != nil {
		return time.Time{}, "", false
	}
	return t, rest, true
}

// stepSection returns the lines of raw that belong to a step that ran from
// start to end. The API reports times truncated to the second, so lines up to
// one second past end are kept. A line without a timestamp belongs with the
// line before it. When the window is unusable or selects nothing, the whole
// log is returned: a longer excerpt is better than an empty one.
func stepSection(raw string, start, end time.Time) string {
	raw = strings.TrimPrefix(raw, byteOrderMark)
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return raw
	}
	limit := end.Add(time.Second)
	var kept strings.Builder
	keep := false
	for _, line := range strings.SplitAfter(raw, "\n") {
		if t, _, ok := splitTimestamp(strings.TrimSuffix(line, "\n")); ok {
			keep = !t.Before(start) && t.Before(limit)
		}
		if keep {
			kept.WriteString(line)
		}
	}
	if kept.Len() == 0 {
		return raw
	}
	return kept.String()
}

// stripFraming removes GitHub's per-line timestamps and workflow command
// markers, and nothing else. Sanitizing and capping is failure.TrimLog's job.
func stripFraming(section string) string {
	var out strings.Builder
	out.Grow(len(section))
	for _, piece := range strings.SplitAfter(section, "\n") {
		line, hadNewline := strings.CutSuffix(piece, "\n")
		if _, rest, ok := splitTimestamp(line); ok {
			line = rest
		}
		if line == "##[endgroup]" {
			continue
		}
		out.WriteString(stripMarker(line))
		if hadNewline {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

var markerReplacements = []struct{ prefix, with string }{
	{"##[group]", ""},
	{"##[command]", ""},
	{"[command]", ""},
	{"##[debug]", ""},
	{"##[error]", "error: "},
	{"##[warning]", "warning: "},
	{"##[notice]", "notice: "},
}

func stripMarker(line string) string {
	for _, m := range markerReplacements {
		if rest, ok := strings.CutPrefix(line, m.prefix); ok {
			return m.with + rest
		}
	}
	return line
}
