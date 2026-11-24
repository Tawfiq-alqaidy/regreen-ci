package github

import (
	"os"
	"strings"
	"testing"
	"time"
)

const bom = "\xef\xbb\xbf"

var (
	step3Start = time.Date(2026, 11, 24, 10, 0, 4, 0, time.UTC)
	step3End   = time.Date(2026, 11, 24, 10, 0, 9, 0, time.UTC)
)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestFixturesKeepTheirBOM makes sure an editor did not silently strip the
// byte order mark the real logs start with.
func TestFixturesKeepTheirBOM(t *testing.T) {
	for _, name := range []string{"job_lint_failed.log", "job_no_timestamps.log"} {
		t.Run(name, func(t *testing.T) {
			raw := readFixture(t, name)
			if !strings.HasPrefix(raw, bom) {
				t.Fatalf("%s lost its UTF-8 BOM", name)
			}
			if n := strings.Count(raw, "\n"); n >= 60 {
				t.Fatalf("%s has %d lines, want fewer than 60", name, n)
			}
		})
	}
}

func TestStepSection(t *testing.T) {
	lint := readFixture(t, "job_lint_failed.log")
	plain := readFixture(t, "job_no_timestamps.log")
	wholeLint := strings.TrimPrefix(lint, bom)
	wholePlain := strings.TrimPrefix(plain, bom)
	t0 := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		raw        string
		start, end time.Time
		want       string
	}{
		{"zero start", lint, time.Time{}, step3End, wholeLint},
		{"zero end", lint, step3Start, time.Time{}, wholeLint},
		{"end before start", lint, step3End, step3Start, wholeLint},
		{"no timestamps", plain, step3Start, step3End, wholePlain},
		{"window matches nothing", lint, t0.Add(time.Hour), t0.Add(2 * time.Hour), wholeLint},
		{"BOM removed", bom + "2026-11-24T10:00:04.0000000Z a\n", step3Start, step3End, "2026-11-24T10:00:04.0000000Z a\n"},
		{"one BOM only", bom + bom + "x\n", time.Time{}, time.Time{}, bom + "x\n"},
		{
			"continuation lines stay with their line",
			"2026-11-24T10:00:03.0000000Z before\n" +
				"2026-11-24T10:00:05.0000000Z first\nsecond\n  third\n" +
				"2026-11-24T10:00:06.0000000Z next\n" +
				"2026-11-24T10:00:10.0000000Z after\ntail of after\n",
			step3Start, step3End,
			"2026-11-24T10:00:05.0000000Z first\nsecond\n  third\n2026-11-24T10:00:06.0000000Z next\n",
		},
		{
			"untimestamped lines before the first timestamp are dropped",
			"orphan\n2026-11-24T10:00:05.0000000Z kept\n",
			step3Start, step3End,
			"2026-11-24T10:00:05.0000000Z kept\n",
		},
		{
			"start is inclusive, end plus one second exclusive",
			"2026-11-24T10:00:03.9999999Z a\n2026-11-24T10:00:04.0000000Z b\n2026-11-24T10:00:09.9999999Z c\n2026-11-24T10:00:10.0000000Z d\n",
			step3Start, step3End,
			"2026-11-24T10:00:04.0000000Z b\n2026-11-24T10:00:09.9999999Z c\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stepSection(tt.raw, tt.start, tt.end); got != tt.want {
				t.Errorf("stepSection =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestStepSectionSelectsStepThreeOfTheFixture(t *testing.T) {
	got := stepSection(readFixture(t, "job_lint_failed.log"), step3Start, step3End)
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if first := lines[0]; !strings.HasSuffix(first, "##[group]Run npm run lint") {
		t.Errorf("first line = %q", first)
	}
	if last := lines[len(lines)-1]; !strings.Contains(last, "10:00:09.9000000Z") || !strings.Contains(last, "##[error]Process completed") {
		t.Errorf("last line = %q, want the 10:00:09.9 line", last)
	}
	for _, bad := range []string{"src/secret.ts", "Post job cleanup", "Set up job", bom} {
		if strings.Contains(got, bad) {
			t.Errorf("section contains %q", bad)
		}
	}
	if !strings.Contains(got, "src/a.ts") {
		t.Error("section misses src/a.ts")
	}
}

func TestStripFraming(t *testing.T) {
	const ts = "2026-11-24T10:00:04.1234567Z "
	tests := []struct {
		name, in, want string
	}{
		{"timestamp removed", ts + "hello world\n", "hello world\n"},
		{"only one space after the timestamp", ts + "  indented\n", "  indented\n"},
		{"timestamp alone", "2026-11-24T10:00:04.1234567Z\nx\n", "\nx\n"},
		{"endgroup dropped", ts + "a\n" + ts + "##[endgroup]\n" + ts + "b\n", "a\nb\n"},
		{"endgroup without timestamp dropped", "a\n##[endgroup]\nb", "a\nb"},
		{"group", ts + "##[group]Run make\n", "Run make\n"},
		{"command marker", ts + "##[command]git status\n", "git status\n"},
		{"bare command marker", ts + "[command]/usr/bin/git log\n", "/usr/bin/git log\n"},
		{"debug", ts + "##[debug]Evaluating\n", "Evaluating\n"},
		{"error", ts + "##[error]Process completed with exit code 1.\n", "error: Process completed with exit code 1.\n"},
		{"warning", ts + "##[warning]careful\n", "warning: careful\n"},
		{"notice", ts + "##[notice]fyi\n", "notice: fyi\n"},
		{"marker in the middle untouched", ts + "see ##[error] in the middle\n", "see ##[error] in the middle\n"},
		{"endgroup text in the middle untouched", ts + "x ##[endgroup]\n", "x ##[endgroup]\n"},
		{"line without timestamp keeps its text", "plain ##[group]x\n", "plain ##[group]x\n"},
		{"non timestamp first field kept", "not-a-time text\n", "not-a-time text\n"},
		{"ANSI untouched", ts + "\x1b[31mred\x1b[0m\n", "\x1b[31mred\x1b[0m\n"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripFraming(tt.in); got != tt.want {
				t.Errorf("stripFraming(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
