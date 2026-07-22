package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunnerPlanAddsWithoutWritingTargets(t *testing.T) {
	root := writePlanCatalogFixture(t)
	runner, stdout, stderr, agentHome := planRunner(t)
	catalogPath := filepath.Join(root, "agx.yaml")

	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("lock code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"plan", "--catalog", catalogPath, "--json"}); code != ExitSuccess {
		t.Fatalf("plan code = %d, stderr = %q", code, stderr.String())
	}
	var report planReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Changes) != 1 || report.Changes[0].Action != "add" || report.Summary.Add != 1 {
		t.Fatalf("plan report = %#v", report)
	}
	if _, err := os.Stat(filepath.Join(agentHome, "skills")); !os.IsNotExist(err) {
		t.Fatalf("plan wrote target directory; stat error = %v", err)
	}
}

func TestRunnerPlanRequiresExplicitAdoption(t *testing.T) {
	root := writePlanCatalogFixture(t)
	runner, stdout, stderr, agentHome := planRunner(t)
	catalogPath := filepath.Join(root, "agx.yaml")
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("lock code = %d, stderr = %q", code, stderr.String())
	}
	targetRoot := filepath.Join(agentHome, "skills", "code-review")
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(root, "skills", "code-review", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetRoot, "SKILL.md"), source, 0o644); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"plan", "--catalog", catalogPath}); code != ExitTargetConflict {
		t.Fatalf("plan code = %d, want %d; stderr = %q", code, ExitTargetConflict, stderr.String())
	}
	if !strings.Contains(stdout.String(), "rerun with --adopt") {
		t.Fatalf("plan stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"plan", "--catalog", catalogPath, "--adopt", "--json"}); code != ExitSuccess {
		t.Fatalf("adopt plan code = %d, stderr = %q", code, stderr.String())
	}
	var report planReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Changes) != 1 || report.Changes[0].Action != "adopt" || report.Summary.Adopt != 1 {
		t.Fatalf("adopt plan report = %#v", report)
	}
	if err := os.WriteFile(filepath.Join(targetRoot, "SKILL.md"), []byte("different\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"plan", "--catalog", catalogPath, "--adopt"}); code != ExitTargetConflict {
		t.Fatalf("different adopt plan code = %d, want %d", code, ExitTargetConflict)
	}
	if !strings.Contains(stdout.String(), "unmanaged target content differs") {
		t.Fatalf("plan stdout = %q", stdout.String())
	}
}

func TestRunnerPlanRejectsSourceDrift(t *testing.T) {
	root := writePlanCatalogFixture(t)
	runner, stdout, stderr, _ := planRunner(t)
	catalogPath := filepath.Join(root, "agx.yaml")
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("lock code = %d, stderr = %q", code, stderr.String())
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "code-review", "SKILL.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"plan", "--catalog", catalogPath}); code != ExitLockOutdated {
		t.Fatalf("plan code = %d, want %d; stderr = %q", code, ExitLockOutdated, stderr.String())
	}
	if !strings.Contains(stderr.String(), "LOCK_OUTDATED") {
		t.Fatalf("stderr = %q, want LOCK_OUTDATED", stderr.String())
	}
}

func TestRunnerPlanRequiresConfiguredAgent(t *testing.T) {
	root := writePlanCatalogFixture(t)
	var stdout, stderr bytes.Buffer
	runner := New(&stdout, &stderr, "dev")
	catalogPath := filepath.Join(root, "agx.yaml")
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("lock code = %d, stderr = %q", code, stderr.String())
	}
	t.Setenv("PATH", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"plan", "--catalog", catalogPath}); code != ExitAgentUnavailable {
		t.Fatalf("plan code = %d, want %d; stderr = %q", code, ExitAgentUnavailable, stderr.String())
	}
	if !strings.Contains(stderr.String(), "AGX_AGENT_UNAVAILABLE") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestValidateTargetRootsRejectsOverlap(t *testing.T) {
	root := t.TempDir()
	if err := validateTargetRoots(map[string]string{
		"codex":  filepath.Join(root, "skills"),
		"claude": filepath.Join(root, "skills", "nested"),
	}); err == nil {
		t.Fatal("validateTargetRoots() error = nil, want overlap error")
	}
	if err := validateTargetRoots(map[string]string{
		"codex":  filepath.Join(root, "codex", "skills"),
		"claude": filepath.Join(root, "claude", "skills"),
	}); err != nil {
		t.Fatalf("validateTargetRoots() error = %v for separate roots", err)
	}
}

func writePlanCatalogFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	skillRoot := filepath.Join(root, "skills", "code-review")
	if err := os.MkdirAll(skillRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte("# Code review\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	catalogYAML := `apiVersion: agx.dev/v1alpha1
kind: Catalog
metadata:
  name: personal
skills:
  code-review:
    source:
      type: local
      path: skills/code-review
    targets:
      codex: {}
`
	if err := os.WriteFile(filepath.Join(root, "agx.yaml"), []byte(catalogYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func planRunner(t *testing.T) (*Runner, *bytes.Buffer, *bytes.Buffer, string) {
	t.Helper()
	binDirectory := t.TempDir()
	executable := filepath.Join(binDirectory, "codex")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))
	agentHome := t.TempDir()
	t.Setenv("CODEX_HOME", agentHome)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	return New(stdout, stderr, "dev"), stdout, stderr, agentHome
}
