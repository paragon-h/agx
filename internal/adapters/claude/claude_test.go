package claude

import (
	"context"
	"path/filepath"
	"testing"
)

func TestResolvePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	paths, err := New().ResolvePaths(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "skills"); paths.SkillsDir != want {
		t.Fatalf("SkillsDir = %q, want %q", paths.SkillsDir, want)
	}
	if want := filepath.Join(home, "CLAUDE.md"); paths.InstructionsFile != want {
		t.Fatalf("InstructionsFile = %q, want %q", paths.InstructionsFile, want)
	}
}

func TestResolvePathsUsesDefaultClaudeDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	paths, err := New().ResolvePaths(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	claudeHome := filepath.Join(home, ".claude")
	if want := filepath.Join(claudeHome, "skills"); paths.SkillsDir != want {
		t.Fatalf("SkillsDir = %q, want %q", paths.SkillsDir, want)
	}
	if want := filepath.Join(claudeHome, "CLAUDE.md"); paths.InstructionsFile != want {
		t.Fatalf("InstructionsFile = %q, want %q", paths.InstructionsFile, want)
	}
}
