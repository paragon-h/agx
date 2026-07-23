package installer

import (
	"path/filepath"
	"testing"
)

func TestSaveLoadAndDeleteJournal(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGX_STATE_HOME", root)
	journal := Journal{
		ID:    "transaction-1",
		State: StatePrepared,
		Targets: []TargetChange{{
			Agent:      "codex",
			Action:     "add",
			TargetPath: filepath.Join(root, "codex", "skills", "example"),
			StagePath:  filepath.Join(root, "codex", "skills", ".agx-stage-test", "content"),
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
