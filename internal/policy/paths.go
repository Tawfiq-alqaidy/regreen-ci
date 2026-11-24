package policy

import (
	"fmt"
	"path"
	"strings"
	"unicode/utf8"
)

// Limits on a repo-relative path. Git itself allows longer paths, but a fix
// that needs one is not a small fix, and the limits bound the matcher's work.
const (
	maxPathBytes    = 4096
	maxPathSegments = 255
)

// checkRepoPath reports why p is not a clean, repo-relative, slash-separated
// path that is safe to judge and to print, or returns nil. The parser and
// Evaluate both call it, so the two checks cannot drift apart.
//
// It rejects control characters (bytes below 0x20 and DEL) and invalid
// UTF-8 so that no path can forge a log line when printed.
func checkRepoPath(p string) error {
	switch {
	case p == "":
		return fmt.Errorf("empty path")
	case len(p) > maxPathBytes:
		return fmt.Errorf("path %q is longer than %d bytes", clip(p), maxPathBytes)
	case !utf8.ValidString(p):
		return fmt.Errorf("path %q is not valid UTF-8", clip(p))
	case hasControl(p):
		return fmt.Errorf("path %q contains a control character", clip(p))
	case strings.Contains(p, `\`):
		return fmt.Errorf("path %q contains a backslash", clip(p))
	case strings.HasPrefix(p, "/"):
		return fmt.Errorf("absolute path %q", clip(p))
	case p == "." || p == ".." || strings.HasPrefix(p, "../") || p != path.Clean(p):
		return fmt.Errorf("path %q is not a clean repo-relative path", clip(p))
	case strings.Count(p, "/")+1 > maxPathSegments:
		return fmt.Errorf("path %q has more than %d segments", clip(p), maxPathSegments)
	}
	return nil
}

// hasControl reports whether s contains a byte below 0x20 or DEL (0x7f).
func hasControl(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}
