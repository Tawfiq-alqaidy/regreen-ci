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
func MatchPath(pattern, p string) bool {
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

	// Anchored: the pattern covers the path or any leading directory prefix.
	for n := 1; n <= len(pathSegs); n++ {
		if matchSegments(pp.segments, pathSegs[:n]) {
			return true
		}
	}
	return false
}

// matchSegments matches pattern segments against path segments in full.
// A ** pattern segment matches zero or more path segments.
func matchSegments(pat, segs []string) bool {
	for len(pat) > 0 {
		if pat[0] == doubleStar {
			rest := pat[1:]
			for skip := 0; skip <= len(segs); skip++ {
				if matchSegments(rest, segs[skip:]) {
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
