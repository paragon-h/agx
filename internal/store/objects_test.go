package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alanhuangch/agx/internal/contenthash"
)

func TestObjectsAndRemove(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGX_STORE_HOME", root)
	firstSource := writeFixture(t, "first\n")
	secondSource := writeFixture(t, "second\n")
	first, err := contenthash.Directory(firstSource)
	if err != nil {
		t.Fatal(err)
	}
	second, err := contenthash.Directory(secondSource)
	if err != nil {
		t.Fatal(err)
	}
	if err := Put(firstSource, first); err != nil {
		t.Fatal(err)
	}
	if err := Put(secondSource, second); err != nil {
		t.Fatal(err)
	}
	objects, issues, err := Objects()
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 2 || len(issues) != 0 || objects[0].Size == 0 || objects[1].Size == 0 {
		t.Fatalf("objects = %#v, issues = %#v", objects, issues)
	}
	if err := Remove(first); err != nil {
		t.Fatal(err)
	}
	if err := Verify(first); err == nil {
		t.Fatal("removed object still verifies")
	}
	if err := Verify(second); err != nil {
		t.Fatalf("remaining object: %v", err)
	}
}

func TestObjectsReportsMalformedEntries(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGX_STORE_HOME", root)
	invalid := filepath.Join(root, "objects", "sha256", "zz")
	if err := os.MkdirAll(invalid, 0o700); err != nil {
		t.Fatal(err)
	}
	objects, issues, err := Objects()
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 0 || len(issues) != 1 || issues[0].Path != invalid {
		t.Fatalf("objects = %#v, issues = %#v", objects, issues)
	}
}
