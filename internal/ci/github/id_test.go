package github

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

func TestEncodeID(t *testing.T) {
	if got := encodeID(12, 34, 5); got != "gha-12-34-5" {
		t.Fatalf("encodeID = %q", got)
	}
}

func TestIDRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		run, job int64
		step     int
	}{{1, 0, 0}, {42, 99, 7}, {math.MaxInt64, math.MaxInt64, 999999999}} {
		id := encodeID(tc.run, tc.job, tc.step)
		run, job, step, err := parseID(id)
		if err != nil {
			t.Fatalf("parseID(%q): %v", id, err)
		}
		if run != tc.run || job != tc.job || step != tc.step {
			t.Fatalf("round trip %q = %d %d %d", id, run, job, step)
		}
	}
}

func TestParseIDRejects(t *testing.T) {
	for _, id := range []string{
		"", "gha-1-2", "gha-1-2-3-4", "gha--1-2-3", "gha-0-1-1", "gha-1-2-x",
		"GHA-1-2-3", " gha-1-2-3", "gha-1-2-3\n", "gha-1-2-1234567890",
		"gha-9223372036854775808-1-1", "gha-1-9223372036854775808-1",
		"gha-12345678901234567890-1-1", "gha-1-2-+3", "gha-1-2-3 ",
		"gha-1-2-" + strconv.Itoa(math.MaxInt32) + "0",
	} {
		t.Run(id, func(t *testing.T) {
			if _, _, _, err := parseID(id); err == nil {
				t.Fatalf("parseID(%q) accepted", id)
			}
		})
	}
}

func TestParseIDErrorQuotesInput(t *testing.T) {
	_, _, _, err := parseID("bad\nid")
	if err == nil || err.Error() == "" {
		t.Fatal("expected error")
	}
	if want := strconv.Quote("bad\nid"); !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not quote input as %s", err, want)
	}
}
