package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paragon-h/agx/internal/contenthash"
	"github.com/paragon-h/agx/internal/installer"
)

func TestRunnerRepairRestoresInterruptedAdd(t *testing.T) {
	runner, stdout, stderr, _ := planRunner(t)
	stateRoot := os.Getenv("AGX_STATE_HOME")
	target := filepath.Join(t.TempDir(), "skills", "review")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("# Added\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	digest, err := contenthash.Directory(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := installer.SaveJournal(installer.Journal{
		ID:    "transaction-add",
		State: installer.StateApplying,
		Targets: []installer.TargetChange{{
			Agent:         "codex",
			Action:        "add",
			TargetPath:    target,
			StagePath:     filepath.Join(filepath.Dir(target), ".agx-stage-test", "content"),
			DesiredDigest: digest,
			Switched:      true,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateRoot, "apply.lock"), []byte("2147483647\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runner.Run(context.Background(), []string{"repair", "--force", "--json"}); code != ExitSuccess {
		t.Fatalf("repair code = %d, stderr = %q", code, stderr.String())
	}
	var result repairResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Repaired || result.Transaction != "transaction-add" {
		t.Fatalf("repair result = %#v", result)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("added target remains after repair: %v", err)
	}
}

func TestRunnerDoctorReportsInterruptedTransaction(t *testing.T) {
	root := writeDoctorCatalogFixture(t)
	binDirectory := t.TempDir()
	executable := filepath.Join(binDirectory, "codex")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDirectory)
	agentHome := t.TempDir()
	t.Setenv("CODEX_HOME", agentHome)
	stateRoot := t.TempDir()
	t.Setenv("AGX_STATE_HOME", stateRoot)
	if err := installer.SaveJournal(installer.Journal{
		ID:    "transaction-doctor",
		State: installer.StateRepairRequired,
		Targets: []installer.TargetChange{{
			Agent:         "codex",
			Action:        "add",
			TargetPath:    filepath.Join(agentHome, "skills", "review"),
			StagePath:     filepath.Join(agentHome, "skills", ".agx-stage-test", "content"),
			DesiredDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Switched:      true,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	runner := New(&stdout, &stderr, "dev")
	if code := runner.Run(context.Background(), []string{"doctor", "--catalog", filepath.Join(root, "agx.yaml")}); code != ExitFailure {
		t.Fatalf("doctor code = %d, want %d; stderr = %q", code, ExitFailure, stderr.String())
	}
	if !strings.Contains(stdout.String(), "transaction: transaction-doctor (repair_required)") || !strings.Contains(stdout.String(), "agx repair") {
		t.Fatalf("doctor stdout = %q", stdout.String())
	}
}
