package policy

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	gitHeaderPrefix = "diff --git "
	devNull         = "/dev/null"
	modeSymlink     = "120000"
	modeSubmodule   = "160000"
	maxNumberDigits = 9
)

// ParseUnifiedDiff parses the output of
//
//	git -c core.quotepath=off diff --no-color --no-ext-diff --no-textconv --no-renames <base> <head>
//
// It fails closed: anything it does not recognize is an error naming the
// 1-based patch line, because the policy engine judges whatever the parser
// reports.
func ParseUnifiedDiff(patch string) (Diff, error) {
	if strings.TrimSpace(patch) == "" {
		return Diff{}, nil
	}
	lines := strings.Split(patch, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	p := &parser{lines: lines, seen: make(map[string]bool)}
	var d Diff
	for p.pos < len(p.lines) {
		fc, err := p.section()
		if err != nil {
			return Diff{}, err
		}
		d.Files = append(d.Files, fc)
	}
	return d, nil
}

// parser reads the lines of a patch with a cursor; pos is the index of the
// line to read next.
type parser struct {
	lines []string
	pos   int
	seen  map[string]bool // paths already claimed by an earlier section
}

func (p *parser) errf(idx int, format string, args ...any) error {
	return fmt.Errorf("parse diff: line %d: %s", idx+1, fmt.Sprintf(format, args...))
}

// header returns line idx with one trailing "\r" removed.
func (p *parser) header(idx int) string {
	return strings.TrimSuffix(p.lines[idx], "\r")
}

// fileHeader is what the lines between "diff --git" and the first hunk say.
type fileHeader struct {
	newFile, deleted bool
	oldMode, newMode string
	modes            []string
	minus, plus      string // paths from "---" and "+++" ("" when absent)
	minusNull        bool
	plusNull         bool
	minusLine        int
	plusLine         int
	hasMinus         bool
	hasPlus          bool
	binary           bool
}

// section parses one file section starting at the cursor.
func (p *parser) section() (FileChange, error) {
	start := p.pos
	line := p.header(start)
	rest, ok := strings.CutPrefix(line, gitHeaderPrefix)
	if !ok {
		return FileChange{}, p.errf(start, "expected %q, got %q", "diff --git", clip(line))
	}
	gitPath, err := splitGitLine(rest)
	if err != nil {
		return FileChange{}, p.errf(start, "%v", err)
	}
	p.pos++

	h, err := p.fileHeader()
	if err != nil {
		return FileChange{}, err
	}
	fc, err := p.resolve(start, gitPath, h)
	if err != nil {
		return FileChange{}, err
	}
	if p.seen[fc.Path] {
		return FileChange{}, p.errf(start, "path %q appears in more than one section", fc.Path)
	}
	p.seen[fc.Path] = true

	var span hunkSpan
	for p.pos < len(p.lines) && isHunkHeader(p.header(p.pos)) {
		if h.binary || !h.hasMinus || !h.hasPlus {
			return FileChange{}, p.errf(p.pos, "hunk without text file headers")
		}
		if err := p.hunk(&fc, &span); err != nil {
			return FileChange{}, err
		}
	}
	if err := p.checkMeaningful(start, h, span); err != nil {
		return FileChange{}, err
	}
	if p.pos < len(p.lines) && !strings.HasPrefix(p.header(p.pos), gitHeaderPrefix) {
		return FileChange{}, p.errf(p.pos, "unexpected line %q after hunk", clip(p.header(p.pos)))
	}
	return fc, nil
}

// checkMeaningful rejects sections git never emits: every section must carry
// a mode line, a binary marker, or file headers with at least one hunk, and
// file headers must be followed by a hunk.
func (p *parser) checkMeaningful(start int, h fileHeader, span hunkSpan) error {
	if h.hasMinus && !span.any {
		return p.errf(start, "--- and +++ lines without a hunk")
	}
	if !span.any && !h.binary && !h.newFile && !h.deleted && h.oldMode == "" {
		return p.errf(start, "section has no mode line, binary marker or hunk")
	}
	return nil
}

// parseIndexLine parses the text after "index ": <hex>..<hex> with an
// optional single space and a six-digit octal mode, which it returns.
func parseIndexLine(rest string) (string, error) {
	spec, mode, hasMode := strings.Cut(rest, " ")
	oldHash, newHash, ok := strings.Cut(spec, "..")
	if !ok || !validHash(oldHash) || !validHash(newHash) {
		return "", fmt.Errorf("malformed index line %q", clip("index "+rest))
	}
	if hasMode && !validMode(mode) {
		return "", fmt.Errorf("malformed index line %q", clip("index "+rest))
	}
	return mode, nil
}

// validHash accepts 4 to 64 hex digits (git abbreviates; --full-index gives
// 40 or 64).
func validHash(s string) bool {
	if len(s) < 4 || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

func isHunkHeader(line string) bool {
	return strings.HasPrefix(line, "@@")
}

// fileHeader reads header lines until a hunk, the next section or the end of
// input.
func (p *parser) fileHeader() (fileHeader, error) {
	var h fileHeader
	once := make(map[string]bool)
	for p.pos < len(p.lines) {
		idx := p.pos
		line := p.header(idx)
		if strings.HasPrefix(line, gitHeaderPrefix) || isHunkHeader(line) {
			return h, nil
		}
		key, err := p.headerLine(idx, line, &h)
		if err != nil {
			return h, err
		}
		if once[key] {
			return h, p.errf(idx, "duplicate header line %q", clip(line))
		}
		once[key] = true
		p.pos++
		if key == "GIT binary patch" {
			p.skipBinaryPatch()
			return h, nil
		}
	}
	return h, nil
}

// skipBinaryPatch moves the cursor to the next "diff --git" line or the end.
func (p *parser) skipBinaryPatch() {
	for p.pos < len(p.lines) && !strings.HasPrefix(p.header(p.pos), gitHeaderPrefix) {
		p.pos++
	}
}

// headerLine records one header line in h and returns a key identifying its
// kind, so the caller can reject repeats.
func (p *parser) headerLine(idx int, line string, h *fileHeader) (string, error) {
	switch {
	case strings.HasPrefix(line, "index "):
		mode, err := parseIndexLine(line[len("index "):])
		if err != nil {
			return "", p.errf(idx, "%v", err)
		}
		if mode != "" {
			h.modes = append(h.modes, mode)
		}
		return "index", nil
	case line == "GIT binary patch":
		h.binary = true
		return "GIT binary patch", nil
	case strings.HasPrefix(line, "Binary files ") && strings.HasSuffix(line, " differ"):
		h.binary = true
		return "Binary files", nil
	case strings.HasPrefix(line, "--- "):
		return "---", p.fileName(idx, line[len("--- "):], "a/", &h.minus, &h.minusNull, &h.minusLine, &h.hasMinus)
	case strings.HasPrefix(line, "+++ "):
		return "+++", p.fileName(idx, line[len("+++ "):], "b/", &h.plus, &h.plusNull, &h.plusLine, &h.hasPlus)
	}
	for _, prefix := range []string{"new file mode ", "deleted file mode ", "old mode ", "new mode "} {
		mode, ok := strings.CutPrefix(line, prefix)
		if !ok {
			continue
		}
		if !validMode(mode) {
			return "", p.errf(idx, "invalid mode %q", clip(mode))
		}
		h.modes = append(h.modes, mode)
		switch prefix {
		case "new file mode ":
			h.newFile = true
		case "deleted file mode ":
			h.deleted = true
		case "old mode ":
			h.oldMode = mode
		case "new mode ":
			h.newMode = mode
		}
		return strings.TrimSpace(prefix), nil
	}
	return "", p.errf(idx, "unrecognized header line %q", clip(line))
}

func (p *parser) fileName(idx int, value, prefix string, name *string, null *bool, lineNo *int, has *bool) error {
	*has = true
	*lineNo = idx
	if value == devNull {
		*null = true
		return nil
	}
	decoded, err := headerName(value)
	if err != nil {
		return p.errf(idx, "%v", err)
	}
	rest, ok := strings.CutPrefix(decoded, prefix)
	if !ok {
		return p.errf(idx, "path %q does not start with %q", clip(decoded), prefix)
	}
	*name = rest
	return nil
}

// headerName decodes the name on a "---" or "+++" line. Git appends a tab to
// the name when it contains a space, so one trailing tab is dropped.
func headerName(value string) (string, error) {
	if strings.HasPrefix(value, `"`) {
		name, rest, err := unquoteC(value)
		if err != nil {
			return "", err
		}
		if rest != "" && rest != "\t" {
			return "", fmt.Errorf("unexpected text %q after quoted name", clip(rest))
		}
		return name, nil
	}
	return strings.TrimSuffix(value, "\t"), nil
}

func validMode(mode string) bool {
	if len(mode) != 6 {
		return false
	}
	for i := 0; i < len(mode); i++ {
		if mode[i] < '0' || mode[i] > '7' {
			return false
		}
	}
	return true
}

// resolve combines the "diff --git" path and the header lines into a
// FileChange with no lines yet.
func (p *parser) resolve(start int, gitPath string, h fileHeader) (FileChange, error) {
	fc := FileChange{Kind: Modified, Binary: h.binary}
	if err := p.checkKind(start, h, &fc); err != nil {
		return fc, err
	}
	fc.Special = special(h)

	fc.Path = gitPath
	if h.hasMinus || h.hasPlus {
		if !h.hasMinus || !h.hasPlus {
			return fc, p.errf(start, "only one of the --- and +++ lines is present")
		}
		if h.binary {
			return fc, p.errf(start, "binary change with --- and +++ lines")
		}
		var err error
		if fc.Path, err = p.fileLinesPath(h); err != nil {
			return fc, err
		}
		if fc.Path != gitPath {
			return fc, p.errf(start, "path %q on the diff --git line does not match %q", clip(gitPath), clip(fc.Path))
		}
	}
	if err := checkRepoPath(fc.Path); err != nil {
		return fc, p.errf(start, "%v", err)
	}
	return fc, nil
}

func (p *parser) checkKind(start int, h fileHeader, fc *FileChange) error {
	switch {
	case h.newFile && h.deleted:
		return p.errf(start, "both new file mode and deleted file mode")
	case h.newFile:
		fc.Kind = Added
	case h.deleted:
		fc.Kind = Deleted
	}
	if (h.newFile || h.deleted) && (h.oldMode != "" || h.newMode != "") {
		return p.errf(start, "old/new mode lines together with new/deleted file mode")
	}
	if (h.oldMode == "") != (h.newMode == "") {
		return p.errf(start, "old mode and new mode must come together")
	}
	if h.hasMinus && h.minusNull != (fc.Kind == Added) {
		return p.errf(h.minusLine, "--- line contradicts the file mode lines")
	}
	if h.hasPlus && h.plusNull != (fc.Kind == Deleted) {
		return p.errf(h.plusLine, "+++ line contradicts the file mode lines")
	}
	return nil
}

// fileLinesPath returns the path named by the --- and +++ lines.
func (p *parser) fileLinesPath(h fileHeader) (string, error) {
	switch {
	case h.minusNull:
		return h.plus, nil
	case h.plusNull:
		return h.minus, nil
	case h.minus != h.plus:
		return "", p.errf(h.plusLine, "--- path %q and +++ path %q differ", clip(h.minus), clip(h.plus))
	}
	return h.plus, nil
}

// special reports the special kind of change, symlink and submodule taking
// precedence over a plain mode change.
func special(h fileHeader) string {
	result := ""
	if h.oldMode != "" {
		result = "mode change"
	}
	for _, m := range h.modes {
		if m == modeSymlink && result != "submodule" {
			result = "symlink"
		}
		if m == modeSubmodule {
			result = "submodule"
		}
	}
	return result
}

// splitGitLine recovers the path from the text after "diff --git ". With
// --no-renames both names are the same path P, written a/P b/P.
func splitGitLine(rest string) (string, error) {
	var first, second string
	switch {
	case strings.HasPrefix(rest, `"`):
		name, after, err := unquoteC(rest)
		if err != nil {
			return "", err
		}
		tail, ok := strings.CutPrefix(after, " ")
		if !ok {
			return "", fmt.Errorf("malformed diff --git line %q", clip(rest))
		}
		first, second = name, tail
	case strings.HasSuffix(rest, `"`) && strings.Contains(rest, ` "`):
		before, after, _ := strings.Cut(rest, ` "`)
		first, second = before, `"`+after
	default:
		n := len(rest)
		if n < 5 || (n-5)%2 != 0 {
			return "", fmt.Errorf("malformed diff --git line %q", clip(rest))
		}
		l := (n - 5) / 2
		if rest[2+l] != ' ' {
			return "", fmt.Errorf("malformed diff --git line %q", clip(rest))
		}
		first, second = rest[:2+l], rest[3+l:]
	}
	if strings.HasPrefix(second, `"`) {
		name, after, err := unquoteC(second)
		if err != nil {
			return "", err
		}
		if after != "" {
			return "", fmt.Errorf("unexpected text %q after quoted name", clip(after))
		}
		second = name
	}
	a, okA := strings.CutPrefix(first, "a/")
	b, okB := strings.CutPrefix(second, "b/")
	if !okA || !okB {
		return "", fmt.Errorf("diff --git names %q and %q lack the a/ and b/ prefixes", clip(first), clip(second))
	}
	if a != b {
		return "", fmt.Errorf("diff --git names differ: %q and %q", clip(a), clip(b))
	}
	return a, nil
}

// unquoteC decodes a C-style quoted name at the start of s and returns it
// with the text after the closing quote.
func unquoteC(s string) (string, string, error) {
	var buf []byte
	for i := 1; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			return string(buf), s[i+1:], nil
		case c < 0x20 || c == 0x7f:
			return "", "", fmt.Errorf("control character in quoted name %q", clip(s))
		case c != '\\':
			buf = append(buf, c)
			continue
		}
		i++
		if i >= len(s) {
			break
		}
		if v, ok := simpleEscapes[s[i]]; ok {
			buf = append(buf, v)
			continue
		}
		if s[i] < '0' || s[i] > '3' || i+2 >= len(s) || !isOctal(s[i+1]) || !isOctal(s[i+2]) {
			return "", "", fmt.Errorf("bad escape in quoted name %q", clip(s))
		}
		buf = append(buf, (s[i]-'0')<<6|(s[i+1]-'0')<<3|(s[i+2]-'0'))
		i += 2
	}
	return "", "", fmt.Errorf("unterminated quoted name %q", clip(s))
}

var simpleEscapes = map[byte]byte{
	'\\': '\\', '"': '"', 'a': 7, 'b': 8, 'f': 12, 'n': 10, 'r': 13, 't': 9, 'v': 11,
}

func isOctal(c byte) bool { return c >= '0' && c <= '7' }

// hunk parses one hunk at the cursor, appending its lines to fc. It counts
// lines by the hunk header, so added content that looks like a header stays
// content.
func (p *parser) hunk(fc *FileChange, span *hunkSpan) error {
	headerIdx := p.pos
	oldStart, oldCount, newStart, newCount, err := parseHunkHeader(p.header(headerIdx))
	if err != nil {
		return p.errf(headerIdx, "%v", err)
	}
	if err := span.accept(fc.Kind, oldStart, oldCount, newStart, newCount); err != nil {
		return p.errf(headerIdx, "%v", err)
	}
	if newCount > 0 && newStart < 1 {
		return p.errf(headerIdx, "hunk adds lines starting at line %d", newStart)
	}
	p.pos++
	number := newStart
	for oldCount > 0 || newCount > 0 {
		if p.pos >= len(p.lines) {
			return p.errf(p.pos, "input ends inside the hunk that starts at line %d", headerIdx+1)
		}
		line := p.lines[p.pos]
		if line == "" {
			return p.errf(p.pos, "empty line inside a hunk")
		}
		switch line[0] {
		case ' ':
			if oldCount == 0 || newCount == 0 {
				return p.errf(p.pos, "context line beyond the hunk header counts")
			}
			oldCount--
			newCount--
			number++
		case '-':
			if oldCount == 0 {
				return p.errf(p.pos, "removed line beyond the hunk header counts")
			}
			oldCount--
			fc.Deleted++
		case '+':
			if newCount == 0 {
				return p.errf(p.pos, "added line beyond the hunk header counts")
			}
			newCount--
			fc.Added = append(fc.Added, Line{Number: number, Text: line[1:]})
			number++
		case '\\':
		default:
			return p.errf(p.pos, "unexpected line inside a hunk: %q", clip(line))
		}
		p.pos++
	}
	for p.pos < len(p.lines) && strings.HasPrefix(p.lines[p.pos], `\`) {
		p.pos++
	}
	return nil
}

// hunkSpan remembers where the previous hunk of a file ended on each side.
type hunkSpan struct {
	any              bool
	oldLast, newLast int
}

// accept checks a hunk against the file's kind and the previous hunk, then
// records it. A side with a zero count reports the line before the change as
// its start, so such a side ends at its start; otherwise it ends at
// start+count-1. Each hunk must start strictly after the previous one ended.
func (s *hunkSpan) accept(kind ChangeKind, oldStart, oldCount, newStart, newCount int) error {
	if oldCount == 0 && newCount == 0 {
		return fmt.Errorf("hunk changes nothing")
	}
	if kind == Added && oldCount != 0 {
		return fmt.Errorf("hunk of a new file removes lines")
	}
	if kind == Deleted && newCount != 0 {
		return fmt.Errorf("hunk of a deleted file adds lines")
	}
	if s.any && (oldStart <= s.oldLast || newStart <= s.newLast) {
		return fmt.Errorf("hunk repeats or overlaps the previous hunk")
	}
	s.any = true
	s.oldLast = endOfSide(oldStart, oldCount)
	s.newLast = endOfSide(newStart, newCount)
	return nil
}

func endOfSide(start, count int) int {
	if count == 0 {
		return start
	}
	return start + count - 1
}

// parseHunkHeader parses "@@ -a[,b] +c[,d] @@[ text]".
func parseHunkHeader(line string) (oldStart, oldCount, newStart, newCount int, err error) {
	bad := fmt.Errorf("malformed hunk header %q", clip(line))
	rest, ok := strings.CutPrefix(line, "@@ -")
	if !ok {
		return 0, 0, 0, 0, bad
	}
	oldSpec, rest, ok := strings.Cut(rest, " +")
	if !ok {
		return 0, 0, 0, 0, bad
	}
	newSpec, tail, ok := strings.Cut(rest, " @@")
	if !ok || (tail != "" && !strings.HasPrefix(tail, " ")) {
		return 0, 0, 0, 0, bad
	}
	if oldStart, oldCount, err = parseRange(oldSpec); err != nil {
		return 0, 0, 0, 0, bad
	}
	if newStart, newCount, err = parseRange(newSpec); err != nil {
		return 0, 0, 0, 0, bad
	}
	return oldStart, oldCount, newStart, newCount, nil
}

// parseRange parses "start[,count]"; an omitted count is 1.
func parseRange(spec string) (start, count int, err error) {
	startText, countText, hasCount := strings.Cut(spec, ",")
	if start, err = parseNumber(startText); err != nil {
		return 0, 0, err
	}
	if !hasCount {
		return start, 1, nil
	}
	if count, err = parseNumber(countText); err != nil {
		return 0, 0, err
	}
	return start, count, nil
}

func parseNumber(s string) (int, error) {
	if s == "" || len(s) > maxNumberDigits {
		return 0, fmt.Errorf("bad number %q", clip(s))
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, fmt.Errorf("bad number %q", clip(s))
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("bad number %q: %w", clip(s), err)
	}
	return n, nil
}

// clip shortens s for use in an error message.
func clip(s string) string {
	const limit = 80
	if len(s) > limit {
		return s[:limit] + "..."
	}
	return s
}
