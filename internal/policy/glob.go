// Package policy is the pure policy engine of regreen-ci. It uses only the
// standard library and performs no I/O: no files, network, environment or
// clock. Everything it decides comes from its arguments.
package policy

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

const doubleStar = "**"

// parsedPattern is a validated pattern split into its matching form.
type parsedPattern struct {
	segments []string
	// anyDepth is true for a pattern with no slash that was not anchored:
	// it matches if any single path segment matches.
	anyDepth bool
}

// parsePattern normalizes and validates pattern. It is the single place
// where pattern text is interpreted, so ValidatePattern and MatchPath agree.
func parsePattern(pattern string) (parsedPattern, error) {
	if strings.TrimSpace(pattern) == "" {
		return parsedPattern{}, errors.New("pattern is empty")
	}
	if strings.HasPrefix(pattern, "!") {
		return parsedPattern{}, errors.New("negation with ! is not supported: a pattern can add protection, never remove it")
	}
	if strings.ContainsAny(pattern, "\\\x00\n") {
		return parsedPattern{}, errors.New("pattern must not contain a backslash, NUL byte or newline")
	}

	body, anchored := strings.CutPrefix(pattern, "/")
	body = strings.TrimSuffix(body, "/")
	if body == "" {
		return parsedPattern{}, errors.New("pattern must name something besides /")
	}

	segments := strings.Split(body, "/")
	for _, seg := range segments {
		if err := validateSegment(seg); err != nil {
			return parsedPattern{}, err
		}
	}
	return parsedPattern{
		segments: segments,
		anyDepth: !anchored && len(segments) == 1,
	}, nil
}

func validateSegment(seg string) error {
	switch {
	case seg == "":
		return errors.New("pattern has an empty path segment")
	case seg == "." || seg == "..":
		return fmt.Errorf("pattern segment %q is not allowed", seg)
	case seg == doubleStar:
		return nil
	case strings.Contains(seg, doubleStar):
		return fmt.Errorf("pattern segment %q mixes ** with other characters", seg)
	}
	if _, err := path.Match(seg, ""); err != nil {
		return fmt.Errorf("pattern segment %q is malformed: %w", seg, err)
	}
	return nil
}

// ValidatePattern reports whether pattern is a usable protected-path pattern.
func ValidatePattern(pattern string) error {
	_, err := parsePattern(pattern)
	return err
}

// MatchPath reports whether the repo-relative, slash-separated path is
// covered by pattern. Call ValidatePattern first; an invalid pattern never matches.
//
// An anchored pattern covers a path when it matches the whole path or any
// leading directory prefix of it. Matching runs in
// O(len(pattern segments) x len(path segments)) segment comparisons.
func MatchPath(pattern, p string) bool {
	pp, err := parsePattern(pattern)
	if err != nil {
		return false
	}
	pathSegs := strings.Split(p, "/")

	if pp.anyDepth {
		for _, seg := range pathSegs {
			if segmentMatch(pp.segments[0], seg) {
				return true
			}
		}
		return false
	}
	return matchPrefix(collapseDoubleStars(pp.segments), pathSegs)
}

// collapseDoubleStars replaces each run of consecutive ** segments with one;
// a run matches exactly what a single ** matches.
func collapseDoubleStars(pat []string) []string {
	out := make([]string, 0, len(pat))
	for i, seg := range pat {
		if seg == doubleStar && i > 0 && pat[i-1] == doubleStar {
			continue
		}
		out = append(out, seg)
	}
	return out
}

// matchPrefix reports whether pat matches segs[:n] for some n >= 1. A **
// pattern segment matches zero or more path segments.
//
// It fills the table m[i][j] = "pat[:i] matches segs[:j]" one pattern row at
// a time, keeping only the previous row: a ** row takes m[i-1][j] (match
// nothing) or m[i][j-1] (absorb one more segment); any other row takes
// m[i-1][j-1] when its segment matches segs[j-1]. The last row answers for
// every prefix length at once.
func matchPrefix(pat, segs []string) bool {
	prev := make([]bool, len(segs)+1)
	cur := make([]bool, len(segs)+1)
	prev[0] = true
	for _, ps := range pat {
		if ps == doubleStar {
			cur[0] = prev[0]
			for j := 1; j <= len(segs); j++ {
				cur[j] = prev[j] || cur[j-1]
			}
		} else {
			cur[0] = false
			for j := 1; j <= len(segs); j++ {
				cur[j] = prev[j-1] && segmentMatch(ps, segs[j-1])
			}
		}
		prev, cur = cur, prev
	}
	for _, ok := range prev[1:] {
		if ok {
			return true
		}
	}
	return false
}

// segmentMatch matches one pattern segment against one path segment.
func segmentMatch(pattern, seg string) bool {
	ok, err := path.Match(pattern, seg)
	return err == nil && ok
}
