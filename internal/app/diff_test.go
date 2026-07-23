package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildDirectoryDiff(t *testing.T) {
	left := t.TempDir()
	right := t.TempDir()
	writeDiffFile(t, left, "SKILL.md", "# Before\nShared\n")
	writeDiffFile(t, left, "removed.txt", "removed\n")
	writeDiffFile(t, right, "SKILL.md", "# After\nShared\n")
	writeDiffFile(t, right, "added.txt", "added\n")
	changes, summary, err := buildDirectoryDiff(left, right)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Added != 1 || summary.Modified != 1 || summary.Removed != 1 || len(changes) != 3 {
		t.Fatalf("changes = %#v, summary = %#v", changes, summary)
	}
	var patch string
	for _, change := range changes {
		if change.Path == "SKILL.md" {
			patch = change.Patch
		}
	}
	if !strings.Contains(patch, "-# Before") || !strings.Contains(patch, "+# After") || !strings.Contains(patch, " Shared") {
		t.Fatalf("patch = %q", patch)
	}
}

func writeDiffFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
