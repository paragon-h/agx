package installer

import "testing"

func TestJournalRejectsImpossibleRestore(t *testing.T) {
	journal := Journal{
		ID:    "generation-1",
		State: StateRollingBack,
		Targets: []TargetChange{{
			Agent:      "codex",
			TargetPath: "/tmp/codex/skills/example",
			Restored:   true,
		}},
	}
	if err := journal.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want impossible restore error")
	}
}
