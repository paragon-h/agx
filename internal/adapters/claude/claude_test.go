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
}
