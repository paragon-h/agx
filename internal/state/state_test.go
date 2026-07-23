package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/paragon-h/agx/internal/contenthash"
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

func TestSaveLoadAndResolveGenerationArtifacts(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGX_STATE_HOME", root)
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("# Snapshot\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	digest, err := contenthash.Directory(target)
	if err != nil {
		t.Fatal(err)
	}
	generation := Generation{
		ID:             "generation-snapshot",
		CreatedAt:      "2026-07-23T10:00:00Z",
		CatalogDigest:  testDigest,
		LockfileDigest: testDigest,
		Entries:        []Entry{{Target: "codex", Skill: "personal/review", Path: target, ContentDigest: digest}},
	}
	AssignArtifacts(generation.Entries)
	if err := SaveArtifacts(generation); err != nil {
		t.Fatal(err)
	}
	if err := Save(generation); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := ArtifactPath(loaded.ID, loaded.Entries[0].Artifact)
	if err != nil {
		t.Fatal(err)
	}
	if artifactDigest, err := contenthash.Directory(artifact); err != nil || artifactDigest != digest {
		t.Fatalf("artifact digest = %q, err = %v", artifactDigest, err)
	}
	if err := DeleteArtifacts(generation.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatalf("artifact remains after delete: %v", err)
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

func TestAcquireRepairLockForceReplacesLock(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGX_STATE_HOME", root)
	if err := os.WriteFile(filepath.Join(root, "apply.lock"), []byte("2147483647\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	release, err := AcquireRepairLock(true)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := InspectApplyLock()
	if err != nil {
		t.Fatal(err)
	}
	if lock == nil || lock.PID != os.Getpid() {
		t.Fatalf("repair lock = %#v", lock)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
}
