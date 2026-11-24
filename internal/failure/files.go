package failure

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxTokenBytes = 4096

// maxSpecialPaths bounds the second pass of ExtractFiles. It costs one scan
// of the text per known path whose name contains whitespace or a delimiter;
// with more such paths than this the pass is skipped and those paths are not
// found.
const maxSpecialPaths = 2000

// ExtractFiles returns the known files mentioned in text, sorted and unique,
// at most maxFiles (non-positive means DefaultLimits().MaxFiles). Only paths
// in known count; the filesystem is never consulted.
//
// The first pass splits each line into tokens at whitespace and delimiters
// and looks each token up. A second pass finds the known paths whose names
// contain whitespace or a delimiter ("pages/[id].tsx", "docs/my file.md"),
// which no token can hold; see findSpecial. It is skipped when known has
// more than maxSpecialPaths such paths.
func ExtractFiles(text string, known map[string]struct{}, maxFiles int) []string {
	if len(known) == 0 {
		return nil
	}
	if maxFiles <= 0 {
		maxFiles = DefaultLimits().MaxFiles
	}
	maxKey := 0
	var special []string
	for k := range known {
		maxKey = max(maxKey, len(k))
		if strings.IndexFunc(k, isDelimiter) >= 0 && !strings.Contains(k, "\n") && !hasDotDot(k) {
			special = append(special, k)
		}
	}
	found := map[string]struct{}{}
	for _, line := range strings.Split(text, "\n") {
		for _, tok := range lineTokens(line) {
			if f, ok := lookup(tok, known, maxKey); ok {
				found[f] = struct{}{}
			}
		}
	}
	if len(special) > 0 && len(special) <= maxSpecialPaths {
		findSpecial(text, special, found)
	}
	if len(found) == 0 {
		return nil
	}
	out := make([]string, 0, len(found))
	for f := range found {
		out = append(out, f)
	}
	sort.Strings(out)
	if len(out) > maxFiles {
		out = out[:maxFiles]
	}
	return out
}

func isDelimiter(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune("\"'`()[]{}<>,;=|", r)
}

// lineTokens splits one line into tokens. A line cut by TrimLog ends in the
// line-cut marker, and the token just before it may be a fragment of a longer
// path (a cut "config.json" reads as the known "config.js"), so it is dropped.
func lineTokens(line string) []string {
	content, cut := strings.CutSuffix(line, lineCutMarker)
	toks := strings.FieldsFunc(content, isDelimiter)
	if cut && len(toks) > 0 {
		if r, _ := utf8.DecodeLastRuneInString(content); !isDelimiter(r) {
			toks = toks[:len(toks)-1]
		}
	}
	return toks
}

func lookup(tok string, known map[string]struct{}, maxKey int) (string, bool) {
	if len(tok) > maxTokenBytes {
		return "", false
	}
	tok = cleanToken(strings.ReplaceAll(tok, `\`, "/"))
	if tok == "" || hasDotDot(tok) {
		return "", false
	}
	if _, ok := known[tok]; ok {
		return tok, true
	}
	if !isAbsolute(tok) {
		return "", false
	}
	// Only suffixes no longer than the longest known path can match.
	for i := max(0, len(tok)-maxKey-1); i < len(tok); i++ {
		if tok[i] != '/' {
			continue
		}
		if cand := tok[i+1:]; cand != "" {
			if _, ok := known[cand]; ok {
				return cand, true
			}
		}
	}
	return "", false
}

// cleanToken strips trailing position suffixes (":12", ":12:5", ":" and ".")
// and leading "./" prefixes.
func cleanToken(tok string) string {
	for {
		before := tok
		tok = strings.TrimRight(tok, ":.")
		if i := strings.LastIndexByte(tok, ':'); i >= 0 && i < len(tok)-1 && allDigits(tok[i+1:]) {
			tok = tok[:i]
		}
		if tok == before {
			break
		}
	}
	for strings.HasPrefix(tok, "./") {
		tok = tok[2:]
	}
	return tok
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

func hasDotDot(tok string) bool {
	for start := 0; start <= len(tok); {
		end := strings.IndexByte(tok[start:], '/')
		if end < 0 {
			end = len(tok) - start
		}
		if tok[start:start+end] == ".." {
			return true
		}
		start += end + 1
	}
	return false
}

func isAbsolute(tok string) bool {
	if strings.HasPrefix(tok, "/") {
		return true
	}
	if len(tok) >= 3 && tok[1] == ':' && tok[2] == '/' {
		c := tok[0] | 0x20
		return c >= 'a' && c <= 'z'
	}
	return false
}

const quoteChars = "\"'`"

// findSpecial adds to found each path of paths that text names. An
// occurrence counts when
//   - it ends at the end of a line, before whitespace, a quote or one of
//     ": ( , ; )", but not right where a line was cut (the path may be a
//     fragment of a longer one), and
//   - it starts a line, follows whitespace, a quote or one of "( [ < =", or
//     follows "/" where the text from the start of its first-pass token to
//     the occurrence is an absolute path ("/..." or "X:/...") or only "./"
//     prefixes, without a ".." segment.
//
// Backslashes count as slashes. Each path costs one linear scan of the text.
func findSpecial(text string, paths []string, found map[string]struct{}) {
	norm, cuts := joinLines(text)
	for _, p := range paths {
		if _, ok := found[p]; !ok && namesPath(norm, cuts, p) {
			found[p] = struct{}{}
		}
	}
}

// joinLines returns text with line-cut markers removed and backslashes
// turned into slashes, and the offsets in it where a cut line ends.
func joinLines(text string) (string, map[int]bool) {
	var b strings.Builder
	b.Grow(len(text))
	cuts := map[int]bool{}
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		content, cut := strings.CutSuffix(line, lineCutMarker)
		b.WriteString(strings.ReplaceAll(content, `\`, "/"))
		if cut {
			cuts[b.Len()] = true
		}
	}
	return b.String(), cuts
}

// namesPath reports whether norm holds an occurrence of p with the
// boundaries findSpecial describes. It is a Knuth-Morris-Pratt scan, so it
// runs in time linear in len(norm) + len(p).
func namesPath(norm string, cuts map[int]bool, p string) bool {
	next := kmpTable(p)
	tr := tokenTracker{text: norm}
	k := 0
	for j := 0; j < len(norm); j++ {
		for k > 0 && norm[j] != p[k] {
			k = next[k-1]
		}
		if norm[j] == p[k] {
			k++
		}
		if k == len(p) {
			if bounded(norm, cuts, j+1-len(p), j+1, &tr) {
				return true
			}
			k = next[k-1]
		}
	}
	return false
}

// kmpTable returns, for each prefix p[:i+1], the length of its longest
// proper prefix that is also its suffix.
func kmpTable(p string) []int {
	next := make([]int, len(p))
	for i, k := 1, 0; i < len(p); i++ {
		for k > 0 && p[i] != p[k] {
			k = next[k-1]
		}
		if p[i] == p[k] {
			k++
		}
		next[i] = k
	}
	return next
}

// bounded reports whether norm[i:j] is delimited as findSpecial requires.
// Calls must come with non-decreasing i.
func bounded(norm string, cuts map[int]bool, i, j int, tr *tokenTracker) bool {
	if j == len(norm) || norm[j] == '\n' {
		if cuts[j] {
			return false
		}
	} else if r, _ := utf8.DecodeRuneInString(norm[j:]); !unicode.IsSpace(r) && !strings.ContainsRune(quoteChars+":(,;)", r) {
		return false
	}
	if i == 0 || norm[i-1] == '\n' {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(norm[:i])
	switch {
	case unicode.IsSpace(r) || strings.ContainsRune(quoteChars+"([<=", r):
		return true
	case r == '/':
		return tr.absoluteBefore(i)
	}
	return false
}

// tokenTracker walks text once, left to right, tracking where the current
// first-pass token starts and whether it has had a ".." segment.
type tokenTracker struct {
	text     string
	pos      int // text[:pos] has been walked
	start    int // start of the token that contains pos
	segStart int // start of the current path segment
	dotdot   bool
}

// absoluteBefore reports whether text[start:i], the part of i's token before
// i, is an absolute path or only "./" prefixes, with no ".." segment. text[i-1]
// is '/'. Calls must come with non-decreasing i, which keeps the total work
// linear.
func (t *tokenTracker) absoluteBefore(i int) bool {
	for t.pos < i {
		r, size := utf8.DecodeRuneInString(t.text[t.pos:])
		switch {
		case isDelimiter(r):
			t.start, t.segStart, t.dotdot = t.pos+size, t.pos+size, false
		case r == '/':
			if t.text[t.segStart:t.pos] == ".." {
				t.dotdot = true
			}
			t.segStart = t.pos + 1
		}
		t.pos += size
	}
	if t.dotdot {
		return false
	}
	prefix := t.text[t.start:i]
	if isAbsolute(prefix) {
		return true
	}
	for strings.HasPrefix(prefix, "./") {
		prefix = prefix[2:]
	}
	return prefix == ""
}
