package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paragon-h/agx/internal/installer"
)

func TestRunnerStatusEmpty(t *testing.T) {
	runner, stdout, stderr, _ := planRunner(t)
	if code := runner.Run(context.Background(), []string{"status", "--json"}); code != ExitSuccess {
		t.Fatalf("status code = %d, stderr = %q", code, stderr.String())
	}
	var report statusReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.State != "empty" || report.Generation != "" || len(report.Entries) != 0 {
		t.Fatalf("status report = %#v", report)
	}
}

func TestRunnerStatusReportsHealthyAndModifiedTargets(t *testing.T) {
	root := writePlanCatalogFixture(t)
	runner, stdout, stderr, agentHome := planRunner(t)
	catalogPath := filepath.Join(root, "agx.yaml")
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("lock code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("apply code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"status", "--json"}); code != ExitSuccess {
		t.Fatalf("healthy status code = %d, stderr = %q", code, stderr.String())
	}
	var healthy statusReport
	if err := json.Unmarshal(stdout.Bytes(), &healthy); err != nil {
		t.Fatal(err)
	}
	if healthy.State != "healthy" || healthy.Generation == "" || healthy.Summary.Healthy != 1 || healthy.Entries[0].State != "healthy" {
		t.Fatalf("healthy status = %#v", healthy)
	}

	manifest := filepath.Join(agentHome, "skills", "code-review", "SKILL.md")
	if err := os.WriteFile(manifest, []byte("external modification\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"status", "--json"}); code != ExitTargetConflict {
		t.Fatalf("modified status code = %d, want %d", code, ExitTargetConflict)
	}
	var modified statusReport
	if err := json.Unmarshal(stdout.Bytes(), &modified); err != nil {
		t.Fatal(err)
	}
	if modified.State != "drifted" || modified.Summary.Modified != 1 || modified.Entries[0].ActualDigest == "" {
		t.Fatalf("modified status = %#v", modified)
	}
}

func TestRunnerStatusReportsMissingTarget(t *testing.T) {
	root := writePlanCatalogFixture(t)
	runner, stdout, stderr, agentHome := planRunner(t)
	catalogPath := filepath.Join(root, "agx.yaml")
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("lock code = %d", code)
	}
	stdout.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("apply code = %d, stderr = %q", code, stderr.String())
	}
	if err := os.RemoveAll(filepath.Join(agentHome, "skills", "code-review")); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"status"}); code != ExitTargetConflict {
		t.Fatalf("status code = %d, want %d", code, ExitTargetConflict)
	}
	if !strings.Contains(stdout.String(), "state: drifted") || !strings.Contains(stdout.String(), "missing\tcodex\tpersonal/code-review") {
		t.Fatalf("status stdout = %q", stdout.String())
	}
}

func TestRunnerStatusReportsUnfinishedTransaction(t *testing.T) {
	runner, stdout, stderr, _ := planRunner(t)
	stateRoot := os.Getenv("AGX_STATE_HOME")
	if err := installer.SaveJournal(installer.Journal{
		ID:    "transaction-interrupted",
		State: installer.StateApplying,
		Targets: []installer.TargetChange{{
			Agent:      "codex",
			Action:     "add",
			TargetPath: filepath.Join(stateRoot, "target"),
			StagePath:  filepath.Join(stateRoot, "stage"),
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if code := runner.Run(context.Background(), []string{"status", "--json"}); code != ExitFailure {
		t.Fatalf("status code = %d, want %d; stderr = %q", code, ExitFailure, stderr.String())
	}
	var report statusReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.State != "repair_required" || report.Transaction == nil || report.Transaction.ID != "transaction-interrupted" {
		t.Fatalf("status report = %#v", report)
	}
}
