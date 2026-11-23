package policy

// ChangeKind says how a file changed.
type ChangeKind int

// The kinds of change a diff can make to a file.
const (
	Modified ChangeKind = iota
	Added
	Deleted
)

// String returns "modified", "added" or "deleted".
func (k ChangeKind) String() string {
	switch k {
	case Modified:
		return "modified"
	case Added:
		return "added"
	case Deleted:
		return "deleted"
	default:
		return "unknown"
	}
}

// Line is one line added by a diff.
type Line struct {
	Number int    // line number in the new version of the file, starting at 1
	Text   string // content without the leading '+' and without the line ending
}

// FileChange is the change to one file.
type FileChange struct {
	Path    string // repo-relative, slash-separated
	Kind    ChangeKind
	Added   []Line // lines added, in order
	Deleted int    // number of lines removed
	Binary  bool   // git reported a binary change; Added and Deleted are empty
	Special string // "", or one of "mode change", "symlink", "submodule"
}

// Diff is a set of file changes, in the order the patch lists them.
type Diff struct {
	Files []FileChange
}

// AddedLines returns the total number of lines the diff adds.
func (d Diff) AddedLines() int {
	n := 0
	for _, f := range d.Files {
		n += len(f.Added)
	}
	return n
}

// DeletedLines returns the total number of lines the diff removes.
func (d Diff) DeletedLines() int {
	n := 0
	for _, f := range d.Files {
		n += f.Deleted
	}
	return n
}
