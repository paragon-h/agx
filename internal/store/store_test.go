package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alanhuangch/agx/internal/contenthash"
)

func TestPutVerifyAndMaterialize(t *testing.T) {
	t.Setenv("AGX_STORE_HOME", t.TempDir())
	source := writeFixture(t, "# Stored\n")
	digest, err := contenthash.Directory(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := Put(source, digest); err != nil {
		t.Fatal(err)
	}
	if err := Put(source, digest); err != nil {
		t.Fatalf("repeat Put() error = %v", err)
	}
	if err := Verify(digest); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "content")
	if err := Materialize(digest, destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# Stored\n" {
		t.Fatalf("materialized content = %q", data)
	}
}

func TestVerifyDetectsMissingAndCorruptObjects(t *testing.T) {
	t.Setenv("AGX_STORE_HOME", t.TempDir())
	source := writeFixture(t, "# Stored\n")
	digest, err := contenthash.Directory(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(digest); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing Verify() error = %v", err)
	}
	if err := Put(source, digest); err != nil {
		t.Fatal(err)
	}
	path, err := ObjectPath(digest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Verify(digest); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("corrupt Verify() error = %v", err)
	}
}

func TestPutRejectsDigestMismatch(t *testing.T) {
	t.Setenv("AGX_STORE_HOME", t.TempDir())
	source := writeFixture(t, "# Stored\n")
	if err := Put(source, "sha256:"+strings.Repeat("0", 64)); err == nil {
		t.Fatal("Put() error = nil, want digest mismatch error")
	}
}

func TestRootRejectsRelativeOverride(t *testing.T) {
	t.Setenv("AGX_STORE_HOME", "relative")
	if _, err := Root(); err == nil {
		t.Fatal("Root() error = nil, want absolute path error")
	}
}

func TestRootDefaultsUnderStateDirectory(t *testing.T) {
	t.Setenv("AGX_STORE_HOME", "")
	stateRoot := t.TempDir()
	t.Setenv("AGX_STATE_HOME", stateRoot)
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(stateRoot, "store"); root != want {
		t.Fatalf("Root() = %q, want %q", root, want)
	}
}

func writeFixture(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}
