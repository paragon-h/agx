package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveLoadAndRemoveReference(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGX_STORE_HOME", root)
	lockPath := filepath.Join(t.TempDir(), "agx.lock")
	first := "sha256:" + strings.Repeat("1", 64)
	second := "sha256:" + strings.Repeat("2", 64)
	if err := SaveReference(lockPath, []string{second, first, second}); err != nil {
		t.Fatal(err)
	}
	references, err := References()
	if err != nil {
		t.Fatal(err)
	}
	if len(references) != 1 || references[0].Lockfile != lockPath || len(references[0].Digests) != 2 || references[0].Digests[0] != first || references[0].Digests[1] != second {
		t.Fatalf("references = %#v", references)
	}
	info, err := os.Stat(references[0].Manifest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("reference permissions = %o, want private file", info.Mode().Perm())
	}
	if err := SaveReference(lockPath, []string{second}); err != nil {
		t.Fatal(err)
	}
	references, err = References()
	if err != nil {
		t.Fatal(err)
	}
	if len(references) != 1 || len(references[0].Digests) != 1 || references[0].Digests[0] != second {
		t.Fatalf("updated references = %#v", references)
	}
	if err := RemoveReference(lockPath); err != nil {
		t.Fatal(err)
	}
	references, err = References()
	if err != nil {
		t.Fatal(err)
	}
	if len(references) != 0 {
		t.Fatalf("references after remove = %#v", references)
	}
}

func TestReferencesRejectMalformedManifest(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGX_STORE_HOME", root)
	directory := filepath.Join(root, "refs", "locks")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, strings.Repeat("0", 64)+".json"), []byte(`{"version":1,"lockfile":"relative","digests":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := References(); err == nil {
		t.Fatal("References() error = nil, want malformed reference error")
	}
}
