package lockfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agx.lock")
	want := Lockfile{
		APIVersion:    APIVersion,
		Kind:          Kind,
		CatalogDigest: testDigest,
		Skills: map[string]LockedSkill{
			"example": {
				Source:        LockedSource{Type: "local", Path: "skills/example"},
				ContentDigest: testDigest,
				LockedAt:      "2026-07-22T10:00:00Z",
			},
		},
	}
	if err := Write(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.CatalogDigest != want.CatalogDigest || got.Skills["example"].ContentDigest != testDigest {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o644 {
		t.Fatalf("lockfile mode = %o, want 644", info.Mode().Perm())
	}
}
