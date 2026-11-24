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
	}{{1, 0, 0}, {42, 99, 7}, {math.MaxInt64, math.MaxInt64, 999999999}, {1, 2, math.MaxInt32 + 1}, {math.MaxInt64, math.MaxInt64, math.MaxInt}} {
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
		"GHA-1-2-3", " gha-1-2-3", "gha-1-2-3\n", "gha-1-2-12345678901234567890",
		"gha-9223372036854775808-1-1", "gha-1-9223372036854775808-1",
		"gha-12345678901234567890-1-1", "gha-1-2-+3", "gha-1-2-3 ",
		"gha-1-2-" + strconv.FormatUint(math.MaxInt+1, 10), "gha-1-2--3",
		"gha-" + strings.Repeat("0", 61) + "1-2-3",
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

func TestParseIDLengthCap(t *testing.T) {
	longest := encodeID(math.MaxInt64, math.MaxInt64, math.MaxInt)
	if len(longest) > maxIDBytes {
		t.Fatalf("longest encodable id is %d bytes, cap %d", len(longest), maxIDBytes)
	}
	long := "gha-1-2-3" + strings.Repeat("Z", 200)
	_, _, _, err := parseID(long)
	if err == nil {
		t.Fatal("over-long id accepted")
	}
	if strings.Contains(err.Error(), "ZZ") || strings.Contains(err.Error(), "gha-") {
		t.Fatalf("error echoes the over-long input: %v", err)
	}
	// An id of exactly the cap is still parsed (and quoted) normally.
	atCap := "gha-1-2-" + strings.Repeat("9", maxIDBytes-len("gha-1-2-"))
	_, _, _, err = parseID(atCap)
	if err == nil || !strings.Contains(err.Error(), strconv.Quote(atCap)) {
		t.Fatalf("err = %v, want it to quote the %d-byte id", err, len(atCap))
	}
}
