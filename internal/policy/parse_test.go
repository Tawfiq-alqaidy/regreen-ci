package policy

import (
	"path"
	"reflect"
	"strings"
	"testing"
)

func TestParseUnifiedDiff(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		patch string
		want  Diff
	}{
		{name: "empty", patch: "", want: Diff{}},
		{name: "newline only", patch: "\n", want: Diff{}},
		{
			name: "modified with context",
			patch: "diff --git a/a.go b/a.go\nindex 1111111..2222222 100644\n--- a/a.go\n+++ b/a.go\n" +
				"@@ -1,3 +1,4 @@ func x()\n one\n-two\n+TWO\n+two2\n three\n",
			want: Diff{Files: []FileChange{{
				Path: "a.go", Kind: Modified, Deleted: 1,
				Added: []Line{{2, "TWO"}, {3, "two2"}},
			}}},
		},
		{
			name: "omitted counts",
			patch: "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n" +
				"@@ -3 +3 @@\n-old\n+new\n",
			want: Diff{Files: []FileChange{{
				Path: "a.go", Kind: Modified, Deleted: 1, Added: []Line{{3, "new"}},
			}}},
		},
		{
			name: "two hunks",
			patch: "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n" +
				"@@ -1,2 +1,3 @@\n a\n+b\n c\n@@ -10 +11,2 @@\n-x\n+y\n+z\n",
			want: Diff{Files: []FileChange{{
				Path: "a.go", Kind: Modified, Deleted: 1,
				Added: []Line{{2, "b"}, {11, "y"}, {12, "z"}},
			}}},
		},
		{
			name: "new file",
			patch: "diff --git a/new.go b/new.go\nnew file mode 100644\nindex 0000000..1111111\n" +
				"--- /dev/null\n+++ b/new.go\n@@ -0,0 +1,2 @@\n+a\n+b\n",
			want: Diff{Files: []FileChange{{
				Path: "new.go", Kind: Added, Added: []Line{{1, "a"}, {2, "b"}},
			}}},
		},
		{
			name: "deleted file",
			patch: "diff --git a/old.go b/old.go\ndeleted file mode 100644\nindex 1111111..0000000\n" +
				"--- a/old.go\n+++ /dev/null\n@@ -1,2 +0,0 @@\n-a\n-b\n",
			want: Diff{Files: []FileChange{{Path: "old.go", Kind: Deleted, Deleted: 2}}},
		},
		{
			name: "two files in order",
			patch: "diff --git a/b.go b/b.go\n--- a/b.go\n+++ b/b.go\n@@ -1 +1 @@\n-a\n+b\n" +
				"diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-c\n+d\n",
			want: Diff{Files: []FileChange{
				{Path: "b.go", Kind: Modified, Deleted: 1, Added: []Line{{1, "b"}}},
				{Path: "a.go", Kind: Modified, Deleted: 1, Added: []Line{{1, "d"}}},
			}},
		},
		{
			name: "no newline markers",
			patch: "diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1,2 +1,2 @@\n x\n-b\n" +
				"\\ No newline at end of file\n+bc\n\\ No newline at end of file\n",
			want: Diff{Files: []FileChange{{
				Path: "a", Kind: Modified, Deleted: 1, Added: []Line{{2, "bc"}},
			}}},
		},
		{
			name:  "binary files line",
			patch: "diff --git a/img.png b/img.png\nindex 1111111..2222222 100644\nBinary files a/img.png and b/img.png differ\n",
			want:  Diff{Files: []FileChange{{Path: "img.png", Kind: Modified, Binary: true}}},
		},
		{
			name: "git binary patch then normal file",
			patch: "diff --git a/bn b/bn\nnew file mode 100644\nindex 0000000..d5d0b8b\nGIT binary patch\n" +
				"literal 3\nKcmb<ms0083<N)#j\n\nliteral 0\nHcmV?d00001\n\n" +
				"diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-a\n+b\n",
			want: Diff{Files: []FileChange{
				{Path: "bn", Kind: Added, Binary: true},
				{Path: "a.go", Kind: Modified, Deleted: 1, Added: []Line{{1, "b"}}},
			}},
		},
		{
			name:  "mode only",
			patch: "diff --git a/exec.sh b/exec.sh\nold mode 100644\nnew mode 100755\n",
			want:  Diff{Files: []FileChange{{Path: "exec.sh", Kind: Modified, Special: "mode change"}}},
		},
		{
			name: "mode change with content",
			patch: "diff --git a/m.sh b/m.sh\nold mode 100644\nnew mode 100755\nindex 1111111..2222222\n" +
				"--- a/m.sh\n+++ b/m.sh\n@@ -1,2 +1,3 @@\n a\n b\n+c\n",
			want: Diff{Files: []FileChange{{
				Path: "m.sh", Kind: Modified, Special: "mode change", Added: []Line{{3, "c"}},
			}}},
		},
		{
			name: "new symlink",
			patch: "diff --git a/link b/link\nnew file mode 120000\nindex 0000000..1111111\n--- /dev/null\n+++ b/link\n" +
				"@@ -0,0 +1 @@\n+b.txt\n\\ No newline at end of file\n",
			want: Diff{Files: []FileChange{{
				Path: "link", Kind: Added, Special: "symlink", Added: []Line{{1, "b.txt"}},
			}}},
		},
		{
			name:  "new submodule",
			patch: "diff --git a/sub b/sub\nnew file mode 160000\nindex 0000000..1111111\n--- /dev/null\n+++ b/sub\n@@ -0,0 +1 @@\n+Subproject commit abc\n",
			want: Diff{Files: []FileChange{{
				Path: "sub", Kind: Added, Special: "submodule", Added: []Line{{1, "Subproject commit abc"}},
			}}},
		},
		{
			name:  "deleted symlink",
			patch: "diff --git a/link b/link\ndeleted file mode 120000\nindex 1111111..0000000\n--- a/link\n+++ /dev/null\n@@ -1 +0,0 @@\n-a\n\\ No newline at end of file\n",
			want:  Diff{Files: []FileChange{{Path: "link", Kind: Deleted, Special: "symlink", Deleted: 1}}},
		},
		{
			name:  "symlink beats mode change",
			patch: "diff --git a/l b/l\nold mode 100644\nnew mode 120000\n",
			want:  Diff{Files: []FileChange{{Path: "l", Kind: Modified, Special: "symlink"}}},
		},
		{
			name: "path with spaces and git trailing tab",
			patch: "diff --git a/my file.txt b/my file.txt\nindex 1111111..2222222 100644\n--- a/my file.txt\t\n+++ b/my file.txt\t\n" +
				"@@ -1 +1,2 @@\n hi\n+more\n",
			want: Diff{Files: []FileChange{{Path: "my file.txt", Kind: Modified, Added: []Line{{2, "more"}}}}},
		},
		{
			name: "path with spaces without trailing tab",
			patch: "diff --git a/my file.txt b/my file.txt\n--- a/my file.txt\n+++ b/my file.txt\n" +
				"@@ -1 +1,2 @@\n hi\n+more\n",
			want: Diff{Files: []FileChange{{Path: "my file.txt", Kind: Modified, Added: []Line{{2, "more"}}}}},
		},
		{
			name:  "mode only with spaces in path",
			patch: "diff --git a/dir sp/a b.sh b/dir sp/a b.sh\nold mode 100644\nnew mode 100755\n",
			want:  Diff{Files: []FileChange{{Path: "dir sp/a b.sh", Kind: Modified, Special: "mode change"}}},
		},
		{
			name: "quoted tab path",
			patch: "diff --git \"a/tab\\there.txt\" \"b/tab\\there.txt\"\nindex 1111111..2222222 100644\n" +
				"--- \"a/tab\\there.txt\"\n+++ \"b/tab\\there.txt\"\n@@ -1 +1,2 @@\n t\n+z\n",
			want: Diff{Files: []FileChange{{Path: "tab\there.txt", Kind: Modified, Added: []Line{{2, "z"}}}}},
		},
		{
			name: "quoted octal path",
			patch: "diff --git \"a/caf\\303\\251.txt\" \"b/caf\\303\\251.txt\"\n" +
				"--- \"a/caf\\303\\251.txt\"\n+++ \"b/caf\\303\\251.txt\"\n@@ -1 +1,2 @@\n c\n+z\n",
			want: Diff{Files: []FileChange{{Path: "café.txt", Kind: Modified, Added: []Line{{2, "z"}}}}},
		},
		{
			name: "quoted path with quote, space and git trailing tab",
			patch: "diff --git \"a/q \\\"s.txt\" \"b/q \\\"s.txt\"\nindex 1111111..2222222 100644\n" +
				"--- \"a/q \\\"s.txt\"\t\n+++ \"b/q \\\"s.txt\"\t\n@@ -1 +1,2 @@\n y\n+z\n",
			want: Diff{Files: []FileChange{{Path: "q \"s.txt", Kind: Modified, Added: []Line{{2, "z"}}}}},
		},
		{
			name:  "quoted mode-only path",
			patch: "diff --git \"a/tab\\tx.sh\" \"b/tab\\tx.sh\"\nold mode 100644\nnew mode 100755\n",
			want:  Diff{Files: []FileChange{{Path: "tab\tx.sh", Kind: Modified, Special: "mode change"}}},
		},
		{
			name: "added lines that look like headers",
			patch: "diff --git a/a.md b/a.md\n--- a/a.md\n+++ b/a.md\n@@ -1 +1,6 @@\n x\n" +
				"+diff --git a/x b/x\n+@@ -1 +1 @@\n+--- a/etc\n++++ b/etc\n+\\ not a marker\n",
			want: Diff{Files: []FileChange{{Path: "a.md", Kind: Modified, Added: []Line{
				{2, "diff --git a/x b/x"}, {3, "@@ -1 +1 @@"}, {4, "--- a/etc"}, {5, "+++ b/etc"}, {6, "\\ not a marker"},
			}}}},
		},
		{
			name: "deleted line that looks like a header",
			patch: "diff --git a/a.md b/a.md\n--- a/a.md\n+++ b/a.md\n@@ -1,2 +1 @@\n x\n" +
				"--- a/etc\n",
			want: Diff{Files: []FileChange{{Path: "a.md", Kind: Modified, Deleted: 1}}},
		},
		{
			name:  "trailing CR kept",
			patch: "diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1 +1 @@\n-x\n+y\r\n",
			want:  Diff{Files: []FileChange{{Path: "a", Kind: Modified, Deleted: 1, Added: []Line{{1, "y\r"}}}}},
		},
		{
			name:  "CRLF headers",
			patch: "diff --git a/a b/a\r\n--- a/a\r\n+++ b/a\r\n@@ -1 +1 @@\r\n-x\n+y\n",
			want:  Diff{Files: []FileChange{{Path: "a", Kind: Modified, Deleted: 1, Added: []Line{{1, "y"}}}}},
		},
		{
			name:  "new empty file",
			patch: "diff --git a/e.txt b/e.txt\nnew file mode 100644\nindex 0000000..e69de29\n",
			want:  Diff{Files: []FileChange{{Path: "e.txt", Kind: Added}}},
		},
		{
			name:  "deleted empty file",
			patch: "diff --git a/e.txt b/e.txt\ndeleted file mode 100644\nindex e69de29..0000000\n",
			want:  Diff{Files: []FileChange{{Path: "e.txt", Kind: Deleted}}},
		},
		{
			name:  "no trailing newline at end of patch",
			patch: "diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1 +1 @@\n-x\n+y",
			want:  Diff{Files: []FileChange{{Path: "a", Kind: Modified, Deleted: 1, Added: []Line{{1, "y"}}}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseUnifiedDiff(tt.patch)
			if err != nil {
				t.Fatalf("ParseUnifiedDiff() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ParseUnifiedDiff() =\n%#v\nwant\n%#v", got, tt.want)
			}
		})
	}
}

func TestParseUnifiedDiffErrors(t *testing.T) {
	t.Parallel()
	const hdr = "diff --git a/x b/x\n--- a/x\n+++ b/x\n"
	tests := []struct {
		name  string
		patch string
	}{
		{"text before first section", "hello\ndiff --git a/x b/x\n"},
		{"rename headers", "diff --git a/a b/b\nsimilarity index 100%\nrename from a\nrename to b\n"},
		{"rename from alone", "diff --git a/a b/a\nrename from a\n"},
		{"similarity index", "diff --git a/a b/a\nsimilarity index 90%\n"},
		{"copy from", "diff --git a/a b/a\ncopy from a\ncopy to b\n"},
		{"unknown header", "diff --git a/x b/x\nfrobnicate yes\n"},
		{"blank line in header", "diff --git a/x b/x\n\n--- a/x\n+++ b/x\n"},
		{"hunk ends early", hdr + "@@ -1,2 +1,2 @@\n a\n"},
		{"hunk ends early, added", hdr + "@@ -1 +1,3 @@\n-a\n+b\n"},
		{"unexpected hunk byte", hdr + "@@ -1 +1 @@\n-a\nx\n"},
		{"empty line in hunk", hdr + "@@ -1,2 +1,2 @@\n a\n\n"},
		{"too many old lines", hdr + "@@ -1 +1 @@\n-a\n-b\n+c\n"},
		{"too many new lines", hdr + "@@ -1 +1 @@\n-a\n+b\n+c\n"},
		{"content after hunk", hdr + "@@ -1 +1 @@\n-a\n+b\nstray\n"},
		{"header after hunk", hdr + "@@ -1 +1 @@\n-a\n+b\n--- a/x\n"},
		{"bad hunk header letters", hdr + "@@ -a,b +c @@\n"},
		{"bad hunk header nonsense", hdr + "@@ nonsense\n"},
		{"hunk header no closing", hdr + "@@ -1 +1\n-a\n+b\n"},
		{"hunk header glued suffix", hdr + "@@ -1 +1 @@x\n-a\n+b\n"},
		{"hunk header negative", hdr + "@@ -1 +-1 @@\n-a\n"},
		{"hunk header overflow", hdr + "@@ -1 +99999999999999999999 @@\n-a\n+b\n"},
		{"hunk without file headers", "diff --git a/x b/x\n@@ -1 +1 @@\n-a\n+b\n"},
		{"only minus header", "diff --git a/x b/x\n--- a/x\n"},
		{"dotdot path", "diff --git a/x b/x\n--- a/x\n+++ b/../outside.txt\n"},
		{"dotdot path git line", "diff --git a/../o b/../o\nold mode 100644\nnew mode 100755\n"},
		{"absolute path", "diff --git a/x b/x\n--- a/x\n+++ b//abs\n"},
		{"missing b prefix", "diff --git a/x b/x\n--- a/x\n+++ outside.txt\n"},
		{"missing a prefix", "diff --git a/x b/x\n--- x\n+++ b/x\n"},
		{"backslash path", "diff --git a/a\\b b/a\\b\n--- a/a\\b\n+++ b/a\\b\n"},
		{"unclean path", "diff --git a/x/./y b/x/./y\nold mode 100644\nnew mode 100755\n"},
		{"empty path", "diff --git a/ b/\nold mode 100644\nnew mode 100755\n"},
		{"NUL in path", "diff --git a/a\x00b b/a\x00b\nold mode 100644\nnew mode 100755\n"},
		{"halves disagree", "diff --git a/x b/y\n"},
		{"halves disagree same length", "diff --git a/x b/y\nold mode 100644\nnew mode 100755\n"},
		{"no b half", "diff --git a/x\n"},
		{"git line gibberish", "diff --git x\n"},
		{"plus path differs from git line", "diff --git a/x b/x\n--- a/x\n+++ b/other\n"},
		{"minus and plus differ", "diff --git a/x b/x\n--- a/y\n+++ b/x\n"},
		{
			"same path twice",
			"diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\n" +
				"diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\n",
		},
		{"deleted mode with dev null old", "diff --git a/x b/x\ndeleted file mode 100644\n--- /dev/null\n+++ b/x\n"},
		{"new mode with plus dev null", "diff --git a/x b/x\nnew file mode 100644\n--- a/x\n+++ /dev/null\n"},
		{"dev null without mode", "diff --git a/x b/x\n--- /dev/null\n+++ b/x\n@@ -0,0 +1 @@\n+a\n"},
		{"both dev null", "diff --git a/x b/x\nnew file mode 100644\n--- /dev/null\n+++ /dev/null\n"},
		{"new and deleted", "diff --git a/x b/x\nnew file mode 100644\ndeleted file mode 100644\n"},
		{"new file with old mode", "diff --git a/x b/x\nnew file mode 100644\nold mode 100644\nnew mode 100755\n"},
		{"old mode alone", "diff --git a/x b/x\nold mode 100644\n"},
		{"bad mode", "diff --git a/x b/x\nnew file mode 12\n"},
		{"duplicate index", "diff --git a/x b/x\nindex 1111111..2222222\nindex 1111111..2222222\n"},
		{"binary with hunks", "diff --git a/x b/x\nBinary files a/x and b/x differ\n@@ -1 +1 @@\n-a\n+b\n"},
		{"binary with file headers", "diff --git a/x b/x\n--- a/x\n+++ b/x\nBinary files a/x and b/x differ\n"},
		{"bad quoted escape", "diff --git \"a/bad\\9\" \"b/bad\\9\"\n"},
		{"unterminated quote", "diff --git \"a/bad b/bad\n"},
		{"quoted plus bad escape", "diff --git a/x b/x\n--- a/x\n+++ \"b/x\\q\"\n"},
		{"quoted trailing junk", "diff --git a/x b/x\n--- a/x\n+++ \"b/x\"junk\n"},
		{"quoted octal too short", "diff --git \"a/x\\30\" \"b/x\\30\"\n"},
		{"quoted octal overflow", "diff --git \"a/x\\777\" \"b/x\\777\"\n"},
		{"quoted NUL", "diff --git \"a/x\\000\" \"b/x\\000\"\n"},
		{"quoted backslash path", "diff --git \"a/x\\\\y\" \"b/x\\\\y\"\n"},
		{"unquoted name without prefix on one side", "diff --git \"a/x\" x\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseUnifiedDiff(tt.patch)
			if err == nil {
				t.Fatalf("ParseUnifiedDiff() = %#v, want error", got)
			}
			if !strings.Contains(err.Error(), "line ") {
				t.Fatalf("error %q does not name a line", err)
			}
		})
	}
}

func TestParseUnifiedDiffErrorLineNumber(t *testing.T) {
	t.Parallel()
	_, err := ParseUnifiedDiff("diff --git a/x b/x\n--- a/x\n+++ b/x\nfrobnicate\n")
	if err == nil || !strings.Contains(err.Error(), "parse diff: line 4:") {
		t.Fatalf("error = %v, want one naming line 4", err)
	}
}

func TestDiffTotalsAndKindString(t *testing.T) {
	t.Parallel()
	d, err := ParseUnifiedDiff("diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1,2 +1,3 @@\n-x\n-y\n+1\n+2\n+3\n" +
		"diff --git a/b b/b\n--- a/b\n+++ b/b\n@@ -1 +1 @@\n-q\n+r\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := d.AddedLines(); got != 4 {
		t.Errorf("AddedLines() = %d, want 4", got)
	}
	if got := d.DeletedLines(); got != 3 {
		t.Errorf("DeletedLines() = %d, want 3", got)
	}
	for k, want := range map[ChangeKind]string{Modified: "modified", Added: "added", Deleted: "deleted", ChangeKind(99): "unknown"} {
		if got := k.String(); got != want {
			t.Errorf("ChangeKind(%d).String() = %q, want %q", int(k), got, want)
		}
	}
}

func FuzzParseUnifiedDiff(f *testing.F) {
	f.Add("diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\n")
	f.Add("")
	f.Fuzz(func(t *testing.T, patch string) {
		d, err := ParseUnifiedDiff(patch)
		if err != nil {
			return
		}
		for _, fc := range d.Files {
			if fc.Path == "" || strings.HasPrefix(fc.Path, "/") || fc.Path != path.Clean(fc.Path) || strings.HasPrefix(fc.Path, "../") || fc.Path == ".." {
				t.Fatalf("accepted unsafe path %q", fc.Path)
			}
			if fc.Deleted < 0 {
				t.Fatalf("negative deleted count")
			}
		}
	})
}
