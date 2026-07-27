package pi

import (
	"context"
	"path/filepath"
	"testing"
)

func TestResolvePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", home)
	paths, err := New().ResolvePaths(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "skills"); paths.SkillsDir != want {
		t.Fatalf("SkillsDir = %q, want %q", paths.SkillsDir, want)
	}
	if want := filepath.Join(home, "AGENTS.md"); paths.InstructionsFile != want {
		t.Fatalf("InstructionsFile = %q, want %q", paths.InstructionsFile, want)
	}
}

func TestResolvePathsExpandsUserHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PI_CODING_AGENT_DIR", "~/.pi-custom")
	paths, err := New().ResolvePaths(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".pi-custom", "skills"); paths.SkillsDir != want {
		t.Fatalf("SkillsDir = %q, want %q", paths.SkillsDir, want)
	}
	if want := filepath.Join(home, ".pi-custom", "AGENTS.md"); paths.InstructionsFile != want {
		t.Fatalf("InstructionsFile = %q, want %q", paths.InstructionsFile, want)
	}
}
