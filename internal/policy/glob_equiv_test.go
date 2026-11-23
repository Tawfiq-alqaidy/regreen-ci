package policy

import (
	"path"
	"strings"
	"testing"
	"time"
)

// referenceMatchPath is the original recursive matcher, kept as an oracle for
// the dynamic-programming one. It is exponential in the number of ** segments
// and must only be fed small inputs.
func referenceMatchPath(pattern, p string) bool {
	pp, err := parsePattern(pattern)
	if err != nil {
		return false
	}
	pathSegs := strings.Split(p, "/")
	if pp.anyDepth {
		for _, seg := range pathSegs {
			if ok, err := path.Match(pp.segments[0], seg); err == nil && ok {
				return true
			}
		}
		return false
	}
	for n := 1; n <= len(pathSegs); n++ {
		if referenceMatchSegments(pp.segments, pathSegs[:n]) {
			return true
		}
	}
	return false
}

func referenceMatchSegments(pat, segs []string) bool {
	for len(pat) > 0 {
		if pat[0] == doubleStar {
			rest := pat[1:]
			for skip := 0; skip <= len(segs); skip++ {
				if referenceMatchSegments(rest, segs[skip:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		if ok, err := path.Match(pat[0], segs[0]); err != nil || !ok {
			return false
		}
		pat, segs = pat[1:], segs[1:]
	}
	return len(segs) == 0
}

// splitMix is a tiny deterministic generator so the equivalence test is
// reproducible without math/rand.
type splitMix uint64

func (s *splitMix) next(n int) int {
	*s += 0x9e3779b97f4a7c15
	z := uint64(*s)
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	z ^= z >> 31
	return int(z>>33) % n
}

func TestMatchPathEquivalentToReference(t *testing.T) {
	t.Parallel()
	patSegs := []string{"a", "b", "ab", "*", "?", "a*", "*b", "[ab]", "[^a]", "**"}
	pathSegs := []string{"a", "b", "ab", "ba", "c", "abc"}
	rng := splitMix(20261124)
	const pairs = 5000
	for i := 0; i < pairs; i++ {
		var pb strings.Builder
		if rng.next(3) == 0 {
			pb.WriteString("/")
		}
		for j, n := 0, 1+rng.next(5); j < n; j++ {
			if j > 0 {
				pb.WriteString("/")
			}
			pb.WriteString(patSegs[rng.next(len(patSegs))])
		}
		if rng.next(4) == 0 {
			pb.WriteString("/")
		}
		segs := make([]string, 1+rng.next(6))
		for j := range segs {
			segs[j] = pathSegs[rng.next(len(pathSegs))]
		}
		pattern, p := pb.String(), strings.Join(segs, "/")
		if got, want := MatchPath(pattern, p), referenceMatchPath(pattern, p); got != want {
			t.Fatalf("MatchPath(%q, %q) = %v, reference = %v", pattern, p, got, want)
		}
	}
}

func TestMatchPathWorstCaseIsFast(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, pattern, path string
	}{
		{
			name:    "review case: 100 repeated test/fixtures segments",
			pattern: "src/**/test/**/fixtures/**/*.snap",
			path:    "src/" + strings.Repeat("test/fixtures/", 100) + "x.txt",
		},
		{
			name:    "8 ** segments against 200 path segments",
			pattern: "**/a/**/b/**/c/**/d/**/e/**/f/**/g/**/z",
			path:    strings.TrimSuffix(strings.Repeat("a/b/c/d/e/f/g/", 28)+"a/b/c/d", "/"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			start := time.Now()
			for range 100 {
				if MatchPath(tt.pattern, tt.path) {
					t.Fatalf("MatchPath(%q, ...) = true, want false", tt.pattern)
				}
			}
			// The quadratic matcher needs milliseconds; the bound is generous so
			// a loaded CI machine does not make the test flaky.
			if elapsed := time.Since(start); elapsed > time.Second {
				t.Fatalf("100 calls took %v, want under 1s", elapsed)
			}
		})
	}
}
