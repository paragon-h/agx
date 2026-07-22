package installer

import "testing"

func TestSaveLoadAndDeleteJournal(t *testing.T) {
	t.Setenv("AGX_STATE_HOME", t.TempDir())
	journal := Journal{
		ID:    "transaction-1",
		State: StatePrepared,
		Targets: []TargetChange{{
			Agent:      "codex",
			Action:     "add",
			TargetPath: "/tmp/codex/skills/example",
			StagePath:  "/tmp/stage/example",
		}},
	}
	if err := SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadJournal()
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || loaded.ID != journal.ID {
		t.Fatalf("LoadJournal() = %#v", loaded)
	}
	if err := DeleteJournal(); err != nil {
		t.Fatal(err)
	}
	loaded, err = LoadJournal()
	if err != nil {
		t.Fatal(err)
	}
	if loaded != nil {
		t.Fatalf("LoadJournal() after delete = %#v", loaded)
	}
}
