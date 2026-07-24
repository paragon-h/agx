package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunnerEmptyCatalogRequiresExplicitRemovalConfirmation(t *testing.T) {
	root := writePlanCatalogFixture(t)
	catalogPath := filepath.Join(root, "agx.yaml")
	runner, stdout, stderr, agentHome := planRunner(t)
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("initial lock code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("initial apply code = %d, stderr = %q", code, stderr.String())
	}
	installedPath := filepath.Join(agentHome, "skills", "code-review")
	if _, err := os.Stat(installedPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalogPath, []byte(`apiVersion: agx.dev/v1alpha1
kind: Catalog
metadata:
  name: personal
skills: {}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("empty lock code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"plan", "--catalog", catalogPath, "--json"}); code != ExitPolicyDenied {
		t.Fatalf("empty plan code = %d, want %d", code, ExitPolicyDenied)
	}
	if !strings.Contains(stderr.String(), "--allow-empty") {
		t.Fatalf("empty plan stderr = %q", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath}); code != ExitPolicyDenied {
		t.Fatalf("empty apply code = %d, want %d", code, ExitPolicyDenied)
	}
	if _, err := os.Stat(installedPath); err != nil {
		t.Fatalf("rejected empty apply changed installed Skill: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"plan", "--catalog", catalogPath, "--allow-empty", "--json"}); code != ExitSuccess {
		t.Fatalf("confirmed empty plan code = %d, stderr = %q", code, stderr.String())
	}
	var report planReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Summary.Remove != 1 {
		t.Fatalf("empty plan = %#v", report)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath, "--allow-empty"}); code != ExitSuccess {
		t.Fatalf("confirmed empty apply code = %d, stderr = %q", code, stderr.String())
	}
	if _, err := os.Stat(installedPath); !os.IsNotExist(err) {
		t.Fatalf("confirmed empty apply retained installed Skill: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"rollback"}); code != ExitSuccess {
		t.Fatalf("rollback after empty apply code = %d, stderr = %q", code, stderr.String())
	}
	if _, err := os.Stat(installedPath); err != nil {
		t.Fatalf("rollback did not restore Skill removed by empty Catalog: %v", err)
	}
}
