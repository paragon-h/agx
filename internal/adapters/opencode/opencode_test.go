package opencode

import (
	"context"
	"path/filepath"
	"testing"
)

func TestResolvePaths(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	paths, err := New().ResolvePaths(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(configHome, "opencode", "skills"); paths.SkillsDir != want {
		t.Fatalf("SkillsDir = %q, want %q", paths.SkillsDir, want)
	}
}

func TestResolvePathsUsesDefaultConfigHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	paths, err := New().ResolvePaths(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".config", "opencode", "skills"); paths.SkillsDir != want {
		t.Fatalf("SkillsDir = %q, want %q", paths.SkillsDir, want)
	}
}
