package installer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/paragon-h/agx/internal/contenthash"
)

func TestRepairJournalRestoresInterruptedUpdate(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGX_STATE_HOME", filepath.Join(root, "state"))
	target := filepath.Join(root, "skills", "review")
	backup := filepath.Join(root, "skills", ".agx-backup-test", "content")
	writeRecoveryDirectory(t, target, "new")
	writeRecoveryDirectory(t, backup, "old")
	desired := recoveryDigest(t, target)
	current := recoveryDigest(t, backup)
	journal := Journal{
		ID:    "transaction-update",
		State: StateApplying,
		Targets: []TargetChange{{
			Agent:         "codex",
			Action:        "update",
			TargetPath:    target,
			StagePath:     filepath.Join(root, "skills", ".agx-stage-test", "content"),
			BackupPath:    backup,
			DesiredDigest: desired,
			CurrentDigest: current,
			Switched:      true,
		}},
	}
	if err := SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	if err := RepairJournal(&journal); err != nil {
		t.Fatal(err)
	}
	if got := readRecoveryValue(t, target); got != "old" {
		t.Fatalf("restored value = %q", got)
	}
	if loaded, err := LoadJournal(); err != nil || loaded != nil {
		t.Fatalf("journal after repair = %#v, err = %v", loaded, err)
	}
}

func TestRepairJournalFinalizesCommittedTransaction(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGX_STATE_HOME", filepath.Join(root, "state"))
	target := filepath.Join(root, "skills", "review")
	writeRecoveryDirectory(t, target, "new")
	journal := Journal{
		ID:    "transaction-committed",
		State: StateCommitted,
		Targets: []TargetChange{{
			Agent:         "codex",
			Action:        "add",
			TargetPath:    target,
			StagePath:     filepath.Join(root, "skills", ".agx-stage-test", "content"),
			DesiredDigest: recoveryDigest(t, target),
			Switched:      true,
		}},
	}
	if err := SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	if err := RepairJournal(&journal); err != nil {
		t.Fatal(err)
	}
	if got := readRecoveryValue(t, target); got != "new" {
		t.Fatalf("committed value = %q", got)
	}
}

func TestRepairJournalPreservesUnexpectedTarget(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGX_STATE_HOME", filepath.Join(root, "state"))
	target := filepath.Join(root, "skills", "review")
	writeRecoveryDirectory(t, target, "unexpected")
	journal := Journal{
		ID:    "transaction-unsafe",
		State: StateApplying,
		Targets: []TargetChange{{
			Agent:         "codex",
			Action:        "add",
			TargetPath:    target,
			StagePath:     filepath.Join(root, "skills", ".agx-stage-test", "content"),
			DesiredDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Switched:      true,
		}},
	}
	if err := SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	if err := RepairJournal(&journal); err == nil {
		t.Fatal("RepairJournal() error = nil")
	}
	if got := readRecoveryValue(t, target); got != "unexpected" {
		t.Fatalf("unexpected target was changed: %q", got)
	}
	loaded, err := LoadJournal()
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || loaded.State != StateRepairRequired {
		t.Fatalf("journal after failed repair = %#v", loaded)
	}
}

func writeRecoveryDirectory(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "value"), []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}

func recoveryDigest(t *testing.T, path string) string {
	t.Helper()
	digest, err := contenthash.Directory(path)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func readRecoveryValue(t *testing.T, path string) string {
	t.Helper()
	value, err := os.ReadFile(filepath.Join(path, "value"))
	if err != nil {
		t.Fatal(err)
	}
	return string(value)
}
