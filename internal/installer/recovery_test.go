package installer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alanhuangch/agx/internal/contenthash"
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

func TestRepairJournalRestoresInterruptedFileUpdate(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGX_STATE_HOME", filepath.Join(root, "state"))
	target := filepath.Join(root, "codex", "AGENTS.md")
	backup := filepath.Join(root, "codex", ".agx-backup-test", "content")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	desired, err := contenthash.File(target)
	if err != nil {
		t.Fatal(err)
	}
	current, err := contenthash.File(backup)
	if err != nil {
		t.Fatal(err)
	}
	journal := Journal{
		ID:    "transaction-file-update",
		State: StateApplying,
		Targets: []TargetChange{{
			Agent:         "codex",
			Kind:          "file",
			Action:        "update",
			TargetPath:    target,
			StagePath:     filepath.Join(root, "codex", ".agx-stage-test", "content"),
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
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "old\n" {
		t.Fatalf("restored file = %q", content)
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
