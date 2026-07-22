package contenthash

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirectoryChangesWithContent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "SKILL.md")
	if err := os.WriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}

	first, err := Directory(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Directory(root)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("stable directory digest changed: %q != %q", first, second)
	}

	if err := os.WriteFile(path, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := Directory(root)
	if err != nil {
		t.Fatal(err)
	}
	if first == changed {
		t.Fatal("directory digest did not change with file content")
	}
}

func TestDirectoryChangesWithEmptyDirectory(t *testing.T) {
	root := t.TempDir()
	before, err := Directory(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	after, err := Directory(root)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("directory digest did not change when an empty directory was added")
	}
}

func TestDirectoryRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := Directory(root); err == nil {
		t.Fatal("Directory() error = nil, want symlink rejection")
	}
}
