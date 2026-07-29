package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alanhuangch/agx/internal/registry"
	"github.com/alanhuangch/agx/internal/state"
)

func TestRunnerPlanAndApplyComposedCatalogs(t *testing.T) {
	t.Setenv("AGX_CONFIG_HOME", t.TempDir())
	t.Setenv("AGX_STORE_HOME", t.TempDir())
	personalPath := writeComposedCatalogFixture(t, "personal", "review", `profiles:
  combined:
    skills:
      include:
        - review
        - work/deploy
    targets:
      - codex
`)
	workPath := writeComposedCatalogFixture(t, "work", "deploy", "")
	registerCatalogFixtures(t, map[string]string{"personal": personalPath, "work": workPath})
	runner, stdout, stderr, agentHome := planRunner(t)
	for _, path := range []string{personalPath, workPath} {
		if code := runner.Run(context.Background(), []string{"lock", "--catalog", path}); code != ExitSuccess {
			t.Fatalf("lock %s code = %d, stderr = %q", path, code, stderr.String())
		}
		stdout.Reset()
		stderr.Reset()
	}

	if code := runner.Run(context.Background(), []string{"plan", "--catalogs", "work,personal", "--profile", "personal/combined", "--json"}); code != ExitSuccess {
		t.Fatalf("plan code = %d, stderr = %q", code, stderr.String())
	}
	var report planReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Profile != "personal/combined" || len(report.Catalogs) != 2 || report.Summary.Add != 2 {
		t.Fatalf("composed plan = %#v", report)
	}
	if report.Changes[0].Skill != "personal/review" || report.Changes[1].Skill != "work/deploy" {
		t.Fatalf("composed changes = %#v", report.Changes)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalogs", "personal,work", "--profile", "combined", "--json"}); code != ExitSuccess {
		t.Fatalf("apply code = %d, stderr = %q", code, stderr.String())
	}
	for _, name := range []string{"review", "deploy"} {
		if _, err := os.Stat(filepath.Join(agentHome, "skills", name, "SKILL.md")); err != nil {
			t.Fatalf("installed %s: %v", name, err)
		}
	}
	current, err := state.Current()
	if err != nil {
		t.Fatal(err)
	}
	if current == nil || current.Profile != "personal/combined" || strings.Join(current.Catalogs, ",") != "personal,work" || len(current.Entries) != 2 {
		t.Fatalf("composed generation = %#v", current)
	}
	reportStatus := buildStatusReport(current, nil)
	if strings.Join(reportStatus.Catalogs, ",") != "personal,work" || reportStatus.Summary.Healthy != 2 {
		t.Fatalf("composed status = %#v", reportStatus)
	}
}

func TestRunnerPlanRejectsComposedTargetNameCollision(t *testing.T) {
	t.Setenv("AGX_CONFIG_HOME", t.TempDir())
	t.Setenv("AGX_STORE_HOME", t.TempDir())
	personalPath := writeComposedCatalogFixture(t, "personal", "review", "")
	workPath := writeComposedCatalogFixture(t, "work", "review", "")
	registerCatalogFixtures(t, map[string]string{"personal": personalPath, "work": workPath})
	runner, stdout, stderr, _ := planRunner(t)
	for _, path := range []string{personalPath, workPath} {
		if code := runner.Run(context.Background(), []string{"lock", "--catalog", path}); code != ExitSuccess {
			t.Fatalf("lock code = %d, stderr = %q", code, stderr.String())
		}
		stdout.Reset()
		stderr.Reset()
	}
	if code := runner.Run(context.Background(), []string{"plan", "--catalogs", "personal,work"}); code != ExitTargetConflict {
		t.Fatalf("plan code = %d, want %d; stderr = %q", code, ExitTargetConflict, stderr.String())
	}
	if !strings.Contains(stderr.String(), "resolve to the same codex target path") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func writeComposedCatalogFixture(t *testing.T, catalogName, skillName, extra string) string {
	t.Helper()
	root := t.TempDir()
	skillRoot := filepath.Join(root, "skills", skillName)
	if err := os.MkdirAll(skillRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte("# "+catalogName+" "+skillName+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	content := `apiVersion: agx.dev/v1alpha1
kind: Catalog
metadata:
  name: ` + catalogName + `
skills:
  ` + skillName + `:
    source:
      type: local
      path: skills/` + skillName + `
    targets:
      codex: {}
` + extra
	path := filepath.Join(root, "agx.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func registerCatalogFixtures(t *testing.T, paths map[string]string) {
	t.Helper()
	value := registry.New()
	for name, path := range paths {
		value.Catalogs[name] = registry.Entry{Path: path}
	}
	if err := registry.Save(value); err != nil {
		t.Fatal(err)
	}
}
