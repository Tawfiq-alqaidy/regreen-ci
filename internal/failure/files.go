package failure

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxTokenBytes = 4096

// ExtractFiles returns the known files mentioned in text, sorted and unique,
// at most maxFiles (non-positive means DefaultLimits().MaxFiles). Only tokens that
// name a file in known count; the filesystem is never consulted. A path
// containing whitespace cannot match, because whitespace splits tokens.
func ExtractFiles(text string, known map[string]struct{}, maxFiles int) []string {
	if len(known) == 0 {
		return nil
	}
	if maxFiles <= 0 {
		maxFiles = DefaultLimits().MaxFiles
	}
	maxKey := 0
	for k := range known {
		maxKey = max(maxKey, len(k))
	}
	found := map[string]struct{}{}
	for _, line := range strings.Split(text, "\n") {
		for _, tok := range lineTokens(line) {
			if f, ok := lookup(tok, known, maxKey); ok {
				found[f] = struct{}{}
			}
		}
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
