package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paragon-h/agx/internal/state"
)

func TestRunnerListSelectsProfile(t *testing.T) {
	root := writeProfileCatalogFixture(t)
	runner, stdout, stderr, _ := planRunner(t)
	catalogPath := filepath.Join(root, "agx.yaml")

	if code := runner.Run(context.Background(), []string{"list", "--catalog", catalogPath, "--profile", "first-only"}); code != ExitSuccess {
		t.Fatalf("list code = %d, stderr = %q", code, stderr.String())
	}
	if got := stdout.String(); got != "personal/first\tlocal\tcodex\n" {
		t.Fatalf("profile list = %q", got)
	}
}

func TestRunnerPlanSelectsProfile(t *testing.T) {
	root := writeProfileCatalogFixture(t)
	runner, stdout, stderr, _ := planRunner(t)
	catalogPath := filepath.Join(root, "agx.yaml")
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("lock code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"plan", "--catalog", catalogPath, "--profile", "first-only", "--json"}); code != ExitSuccess {
		t.Fatalf("plan code = %d, stderr = %q", code, stderr.String())
	}
	var report planReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Profile != "first-only" || len(report.Changes) != 1 || report.Changes[0].Skill != "personal/first" {
		t.Fatalf("profile plan = %#v", report)
	}
}

func TestRunnerApplyProfileRemovesUnselectedSkillsAndRecordsProfile(t *testing.T) {
	root := writeProfileCatalogFixture(t)
	runner, stdout, stderr, agentHome := planRunner(t)
	catalogPath := filepath.Join(root, "agx.yaml")
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("lock code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("full apply code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath, "--profile", "first-only", "--json"}); code != ExitSuccess {
		t.Fatalf("profile apply code = %d, stderr = %q", code, stderr.String())
	}
	var result applyResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Summary.Remove != 1 || result.Summary.Unchanged != 1 {
		t.Fatalf("profile apply result = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(agentHome, "skills", "second")); !os.IsNotExist(err) {
		t.Fatalf("unselected Skill still exists: %v", err)
	}
	current, err := state.Current()
	if err != nil {
		t.Fatal(err)
	}
	if current == nil || current.Profile != "first-only" || len(current.Entries) != 1 || current.Entries[0].Skill != "personal/first" {
		t.Fatalf("profile generation = %#v", current)
	}
	report := buildStatusReport(current, nil)
	if report.Profile != "first-only" {
		t.Fatalf("status profile = %q", report.Profile)
	}
}

func TestRunnerApplyEmptyProfileRequiresConfirmation(t *testing.T) {
	root := writeProfileCatalogFixture(t)
	runner, stdout, stderr, agentHome := planRunner(t)
	catalogPath := filepath.Join(root, "agx.yaml")
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("lock code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("full apply code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath, "--profile", "empty"}); code != ExitPolicyDenied {
		t.Fatalf("empty profile apply code = %d, want %d", code, ExitPolicyDenied)
	}
	if !strings.Contains(stderr.String(), "--allow-empty") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath, "--profile", "empty", "--allow-empty"}); code != ExitSuccess {
		t.Fatalf("confirmed empty apply code = %d, stderr = %q", code, stderr.String())
	}
	for _, name := range []string{"first", "second"} {
		if _, err := os.Stat(filepath.Join(agentHome, "skills", name)); !os.IsNotExist(err) {
			t.Fatalf("Skill %s still exists: %v", name, err)
		}
	}
}

func TestRunnerRejectsUnknownProfile(t *testing.T) {
	root := writeProfileCatalogFixture(t)
	runner, stdout, stderr, _ := planRunner(t)
	catalogPath := filepath.Join(root, "agx.yaml")
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("lock code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"plan", "--catalog", catalogPath, "--profile", "missing"}); code != ExitInvalidConfig {
		t.Fatalf("plan code = %d, want %d", code, ExitInvalidConfig)
	}
	if !strings.Contains(stderr.String(), "AGX_PROFILE_INVALID") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func writeProfileCatalogFixture(t *testing.T) string {
	t.Helper()
	t.Setenv("AGX_STORE_HOME", t.TempDir())
	root := writeTwoSkillCatalogFixture(t)
	catalogYAML := `apiVersion: agx.dev/v1alpha1
kind: Catalog
metadata:
  name: personal
skills:
  first:
    source:
      type: local
      path: skills/first
    targets:
      codex: {}
  second:
    source:
      type: local
      path: skills/second
    targets:
      codex: {}
profiles:
  first-only:
    skills:
      include:
        - first
    targets:
      - codex
  empty:
    skills:
      exclude:
        - first
        - second
`
	if err := os.WriteFile(filepath.Join(root, "agx.yaml"), []byte(catalogYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}
