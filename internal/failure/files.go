package failure

import (
	"sort"
	"strings"
	"unicode"
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
	found := map[string]struct{}{}
	for _, tok := range strings.FieldsFunc(text, isDelimiter) {
		if f, ok := lookup(tok, known); ok {
			found[f] = struct{}{}
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

func lookup(tok string, known map[string]struct{}) (string, bool) {
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
	for i := 0; i < len(tok); i++ {
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
	for _, seg := range strings.Split(tok, "/") {
		if seg == ".." {
			return true
		}
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
