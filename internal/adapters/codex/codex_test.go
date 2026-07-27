package codex

import (
	"context"
	"path/filepath"
	"testing"
)

func TestResolvePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
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
	if want := filepath.Join(home, "AGENTS.override.md"); paths.InstructionsOverrideFile != want {
		t.Fatalf("InstructionsOverrideFile = %q, want %q", paths.InstructionsOverrideFile, want)
	}
	if want := filepath.Join(home, "config.toml"); paths.ConfigFile != want {
		t.Fatalf("ConfigFile = %q, want %q", paths.ConfigFile, want)
	}
}
