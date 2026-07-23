package installer

import (
	"path/filepath"
	"testing"
)

func TestJournalRejectsImpossibleRestore(t *testing.T) {
	root := t.TempDir()
	journal := Journal{
		ID:    "generation-1",
		State: StateRollingBack,
		Targets: []TargetChange{{
			Agent:      "codex",
			Action:     "add",
			TargetPath: filepath.Join(root, "codex", "skills", "example"),
			StagePath:  filepath.Join(root, "codex", "skills", ".agx-stage-test", "content"),
			Restored:   true,
		}},
	}
	if err := journal.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want impossible restore error")
	}
}
