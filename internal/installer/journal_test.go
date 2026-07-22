package installer

import "testing"

func TestJournalRejectsImpossibleRestore(t *testing.T) {
	journal := Journal{
		ID:    "generation-1",
		State: StateRollingBack,
		Targets: []TargetChange{{
			Agent:      "codex",
			Action:     "add",
			TargetPath: "/tmp/codex/skills/example",
			StagePath:  "/tmp/stage/example",
			Restored:   true,
		}},
	}
	if err := journal.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want impossible restore error")
	}
}
