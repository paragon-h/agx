package filetree

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alanhuangch/agx/internal/contenthash"
)

func TestCopyPreservesContentDigest(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(filepath.Join(source, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("# Skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "scripts", "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "destination")
	if err := Copy(source, destination); err != nil {
		t.Fatal(err)
	}
	sourceDigest, err := contenthash.Directory(source)
	if err != nil {
		t.Fatal(err)
	}
	destinationDigest, err := contenthash.Directory(destination)
	if err != nil {
		t.Fatal(err)
	}
	if sourceDigest != destinationDigest {
		t.Fatalf("copied digest = %q, want %q", destinationDigest, sourceDigest)
	}
}
