package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alanhuangch/agx/internal/instructions"
	"github.com/alanhuangch/agx/internal/state"
)

func TestRunnerManagesCodexInstructionsAndPreservesUserContent(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "instructions", "common.md")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("Use version one.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(root, "agx.yaml")
	writeInstructionsCatalog(t, catalogPath, true)
	runner, stdout, stderr, codexHome := planRunner(t)
	target := filepath.Join(codexHome, "AGENTS.md")
	if err := os.WriteFile(target, []byte("# Personal guidance\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	runInstructionsCommand(t, runner, stdout, stderr, "lock", "--catalog", catalogPath)
	runInstructionsCommand(t, runner, stdout, stderr, "apply", "--catalog", catalogPath)
	first, err := state.Current()
	if err != nil || first == nil || len(first.Entries) != 1 {
		t.Fatalf("first generation = %#v, err = %v", first, err)
	}
	if first.Entries[0].Kind != "file" || first.Entries[0].ManagedDigest == "" {
		t.Fatalf("Instructions generation entry = %#v", first.Entries[0])
	}
	assertInstructionsFile(t, target, "# Personal guidance", "Use version one.")

	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	content = []byte(strings.Replace(string(content), "# Personal guidance", "# Personal guidance changed", 1))
	if err := os.WriteFile(target, content, 0o644); err != nil {
		t.Fatal(err)
	}
	runInstructionsCommand(t, runner, stdout, stderr, "status")

	if err := os.WriteFile(source, []byte("Use version two.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runInstructionsCommand(t, runner, stdout, stderr, "lock", "--catalog", catalogPath)
	runInstructionsCommand(t, runner, stdout, stderr, "apply", "--catalog", catalogPath)
	second, err := state.Current()
	if err != nil || second == nil || second.PreviousID != first.ID {
		t.Fatalf("second generation = %#v, err = %v", second, err)
	}
	assertInstructionsFile(t, target, "# Personal guidance changed", "Use version two.")

	runInstructionsCommand(t, runner, stdout, stderr, "rollback", "--generation", first.ID)
	rolledBack, err := state.Current()
	if err != nil || rolledBack == nil {
		t.Fatalf("rollback generation = %#v, err = %v", rolledBack, err)
	}
	assertInstructionsFile(t, target, "# Personal guidance changed", "Use version one.")

	writeInstructionsCatalog(t, catalogPath, false)
	runInstructionsCommand(t, runner, stdout, stderr, "lock", "--catalog", catalogPath)
	runInstructionsCommand(t, runner, stdout, stderr, "apply", "--catalog", catalogPath, "--allow-empty")
	withoutManaged, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(withoutManaged), "# Personal guidance changed") || strings.Contains(string(withoutManaged), instructions.BeginMarker) {
		t.Fatalf("released Instructions file = %q", withoutManaged)
	}
	current, err := state.Current()
	if err != nil || current == nil || len(current.Entries) != 0 {
		t.Fatalf("released generation = %#v, err = %v", current, err)
	}

	runInstructionsCommand(t, runner, stdout, stderr, "rollback", "--generation", rolledBack.ID)
	assertInstructionsFile(t, target, "# Personal guidance changed", "Use version one.")
}

func TestRunnerRejectsNonEmptyCodexInstructionsOverride(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "instructions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "instructions", "common.md"), []byte("Managed.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(root, "agx.yaml")
	writeInstructionsCatalog(t, catalogPath, true)
	runner, stdout, stderr, codexHome := planRunner(t)
	if err := os.WriteFile(filepath.Join(codexHome, "AGENTS.override.md"), []byte("Override.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runInstructionsCommand(t, runner, stdout, stderr, "lock", "--catalog", catalogPath)
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"plan", "--catalog", catalogPath}); code != ExitTargetConflict {
		t.Fatalf("plan code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "takes precedence over AGENTS.md") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"doctor", "--catalog", catalogPath}); code != ExitAgentUnavailable {
		t.Fatalf("doctor code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "instructions override") || !strings.Contains(stdout.String(), "takes precedence over AGENTS.md") {
		t.Fatalf("doctor stdout = %q", stdout.String())
	}
}

func TestRunnerComposesInstructionsAcrossCatalogsDeterministically(t *testing.T) {
	t.Setenv("AGX_CONFIG_HOME", t.TempDir())
	t.Setenv("AGX_STORE_HOME", t.TempDir())
	personalPath := writeInstructionsOnlyCatalog(t, "personal", "Personal instructions.\n")
	workPath := writeInstructionsOnlyCatalog(t, "work", "Work instructions.\n")
	registerCatalogFixtures(t, map[string]string{"work": workPath, "personal": personalPath})
	runner, stdout, stderr, codexHome := planRunner(t)
	for _, path := range []string{workPath, personalPath} {
		runInstructionsCommand(t, runner, stdout, stderr, "lock", "--catalog", path)
	}
	runInstructionsCommand(t, runner, stdout, stderr, "apply", "--catalogs", "work,personal")
	content, err := os.ReadFile(filepath.Join(codexHome, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	personal := strings.Index(string(content), "Personal instructions.")
	work := strings.Index(string(content), "Work instructions.")
	if personal < 0 || work < 0 || personal >= work {
		t.Fatalf("composed Instructions order is not deterministic: %q", content)
	}
}

func TestRunnerAppliesInstructionsToCodexPiAndOpenCode(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "instructions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "instructions", "common.md"), []byte("Shared global instructions.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(root, "agx.yaml")
	catalog := `apiVersion: agx.dev/v1alpha1
kind: Catalog
metadata:
  name: personal
skills: {}
instructions:
  common:
    sources:
      - instructions/common.md
    targets:
      codex: {}
      pi: {}
      opencode: {}
profiles:
  pi-only:
    targets:
      - pi
`
	if err := os.WriteFile(catalogPath, []byte(catalog), 0o644); err != nil {
		t.Fatal(err)
	}

	binDirectory := t.TempDir()
	for _, name := range []string{"codex", "pi", "opencode"} {
		if err := os.WriteFile(filepath.Join(binDirectory, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))
	codexHome := t.TempDir()
	piHome := t.TempDir()
	xdgHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("PI_CODING_AGENT_DIR", piHome)
	t.Setenv("XDG_CONFIG_HOME", xdgHome)
	t.Setenv("AGX_STATE_HOME", t.TempDir())
	t.Setenv("AGX_STORE_HOME", t.TempDir())
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	runner := New(stdout, stderr, "dev")
	runInstructionsCommand(t, runner, stdout, stderr, "lock", "--catalog", catalogPath)

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"plan", "--catalog", catalogPath, "--profile", "pi-only", "--json"}); code != ExitSuccess {
		t.Fatalf("profile plan code = %d, stderr = %q", code, stderr.String())
	}
	var profilePlan planReport
	if err := json.Unmarshal([]byte(stdout.String()), &profilePlan); err != nil {
		t.Fatal(err)
	}
	if len(profilePlan.Changes) != 1 || profilePlan.Changes[0].Target != "pi" {
		t.Fatalf("profile Instructions plan = %#v", profilePlan)
	}

	runInstructionsCommand(t, runner, stdout, stderr, "apply", "--catalog", catalogPath)
	targets := []string{
		filepath.Join(codexHome, "AGENTS.md"),
		filepath.Join(piHome, "AGENTS.md"),
		filepath.Join(xdgHome, "opencode", "AGENTS.md"),
	}
	for _, target := range targets {
		assertInstructionsFile(t, target, "Shared global instructions.")
	}
	current, err := state.Current()
	if err != nil || current == nil || len(current.Entries) != 3 {
		t.Fatalf("multi-target generation = %#v, err = %v", current, err)
	}
}

func TestRunnerAppliesLockedInstructionsWhenSourceIsMissing(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "instructions"), 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "instructions", "common.md")
	if err := os.WriteFile(source, []byte("Locked instructions.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(root, "agx.yaml")
	writeInstructionsCatalog(t, catalogPath, true)
	runner, stdout, stderr, codexHome := planRunner(t)
	runInstructionsCommand(t, runner, stdout, stderr, "lock", "--catalog", catalogPath)
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	runInstructionsCommand(t, runner, stdout, stderr, "apply", "--catalog", catalogPath)
	assertInstructionsFile(t, filepath.Join(codexHome, "AGENTS.md"), "Locked instructions.")
}

func writeInstructionsCatalog(t *testing.T, path string, enabled bool) {
	t.Helper()
	content := `apiVersion: agx.dev/v1alpha1
kind: Catalog
metadata:
  name: personal
skills: {}
`
	if enabled {
		content += `instructions:
  common:
    sources:
      - instructions/common.md
    targets:
      codex: {}
`
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeInstructionsOnlyCatalog(t *testing.T, name, instruction string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "instructions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "instructions", "common.md"), []byte(instruction), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "agx.yaml")
	content := `apiVersion: agx.dev/v1alpha1
kind: Catalog
metadata:
  name: ` + name + `
skills: {}
instructions:
  common:
    sources:
      - instructions/common.md
    targets:
      codex: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func runInstructionsCommand(t *testing.T, runner *Runner, stdout, stderr interface{ Reset() }, args ...string) {
	t.Helper()
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), args); code != ExitSuccess {
		t.Fatalf("%s code = %d", args[0], code)
	}
}

func assertInstructionsFile(t *testing.T, path string, values ...string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range values {
		if !strings.Contains(string(content), value) {
			t.Fatalf("Instructions file %q does not contain %q", content, value)
		}
	}
	if strings.Count(string(content), instructions.BeginMarker) != 1 || strings.Count(string(content), instructions.EndMarker) != 1 {
		t.Fatalf("Instructions file has invalid markers: %q", content)
	}
}
