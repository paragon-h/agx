package state

import (
	"os"
	"path/filepath"
	"testing"
)

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestSaveAndLoadCurrent(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGX_STATE_HOME", root)
	generation := Generation{
		ID:             "generation-1",
		CreatedAt:      "2026-07-23T10:00:00Z",
		CatalogDigest:  testDigest,
		LockfileDigest: testDigest,
		Entries: []Entry{{
			Target:        "codex",
			Skill:         "personal/review",
			Path:          filepath.Join(root, "codex", "review"),
			ContentDigest: testDigest,
		}},
	}
	if err := Save(generation); err != nil {
		t.Fatal(err)
	}
	loaded, err := Current()
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || loaded.ID != generation.ID || len(loaded.Entries) != 1 {
		t.Fatalf("Current() = %#v, want %#v", loaded, generation)
	}
	if _, err := os.Stat(filepath.Join(root, "generations", "generation-1.json")); err != nil {
		t.Fatal(err)
	}
}

func TestCurrentMissingReturnsNil(t *testing.T) {
	t.Setenv("AGX_STATE_HOME", t.TempDir())
	loaded, err := Current()
	if err != nil {
		t.Fatal(err)
	}
	if loaded != nil {
		t.Fatalf("Current() = %#v, want nil", loaded)
	}
}

func TestAcquireApplyLock(t *testing.T) {
	t.Setenv("AGX_STATE_HOME", t.TempDir())
	release, err := AcquireApplyLock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireApplyLock(); err == nil {
		t.Fatal("second AcquireApplyLock() error = nil")
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	secondRelease, err := AcquireApplyLock()
	if err != nil {
		t.Fatal(err)
	}
	if err := secondRelease(); err != nil {
		t.Fatal(err)
	}
}
