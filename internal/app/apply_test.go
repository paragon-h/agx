package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alanhuangch/agx/internal/contenthash"
	"github.com/alanhuangch/agx/internal/installer"
	"github.com/alanhuangch/agx/internal/state"
)

func TestRunnerApplyLocalSkillFromExpandedPath(t *testing.T) {
	for _, mode := range []string{"absolute", "home"} {
		t.Run(mode, func(t *testing.T) {
			catalogRoot := t.TempDir()
			sourceRoot := filepath.Join(t.TempDir(), "review")
			catalogSource := sourceRoot
			if mode == "home" {
				home := t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("USERPROFILE", home)
				sourceRoot = filepath.Join(home, "shared-skills", "review")
				catalogSource = "~/shared-skills/review"
			}
			if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(sourceRoot, "SKILL.md"), []byte("# External review\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			catalogYAML := fmt.Sprintf(`apiVersion: agx.dev/v1alpha1
kind: Catalog
metadata:
  name: personal
skills:
  review:
    source:
      type: local
      path: %q
    targets:
      codex: {}
`, catalogSource)
			catalogPath := filepath.Join(catalogRoot, "agx.yaml")
			if err := os.WriteFile(catalogPath, []byte(catalogYAML), 0o644); err != nil {
				t.Fatal(err)
			}

			runner, stdout, stderr, agentHome := planRunner(t)
			if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
				t.Fatalf("lock code = %d, stderr = %q", code, stderr.String())
			}
			stdout.Reset()
			stderr.Reset()
			if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath}); code != ExitSuccess {
				t.Fatalf("apply code = %d, stderr = %q", code, stderr.String())
			}
			installed, err := os.ReadFile(filepath.Join(agentHome, "skills", "review", "SKILL.md"))
			if err != nil {
				t.Fatal(err)
			}
			if got, want := string(installed), "# External review\n"; got != want {
				t.Fatalf("installed content = %q, want %q", got, want)
			}
		})
	}
}

func TestRunnerApplyToAllBuiltInAgents(t *testing.T) {
	root := t.TempDir()
	skillRoot := filepath.Join(root, "skills", "shared")
	if err := os.MkdirAll(skillRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte("# Shared skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	catalogYAML := `apiVersion: agx.dev/v1alpha1
kind: Catalog
metadata:
  name: personal
skills:
  shared:
    source:
      type: local
      path: skills/shared
    targets:
      codex: {}
      claude: {}
      pi: {}
      opencode: {}
`
	catalogPath := filepath.Join(root, "agx.yaml")
	if err := os.WriteFile(catalogPath, []byte(catalogYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	binDirectory := t.TempDir()
	for _, name := range []string{"codex", "claude", "pi", "opencode"} {
		if err := os.WriteFile(filepath.Join(binDirectory, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))
	codexHome := t.TempDir()
	claudeHome := t.TempDir()
	piHome := t.TempDir()
	xdgHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)
	t.Setenv("PI_CODING_AGENT_DIR", piHome)
	t.Setenv("XDG_CONFIG_HOME", xdgHome)
	t.Setenv("AGX_STATE_HOME", t.TempDir())

	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	runner := New(stdout, stderr, "dev")
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("lock code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath, "--json"}); code != ExitSuccess {
		t.Fatalf("apply code = %d, stderr = %q", code, stderr.String())
	}
	var result applyResult
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatal(err)
	}
	if result.Summary.Add != 4 {
		t.Fatalf("apply result = %#v, want four additions", result)
	}

	targets := []string{
		filepath.Join(codexHome, "skills", "shared", "SKILL.md"),
		filepath.Join(claudeHome, "skills", "shared", "SKILL.md"),
		filepath.Join(piHome, "skills", "shared", "SKILL.md"),
		filepath.Join(xdgHome, "opencode", "skills", "shared", "SKILL.md"),
	}
	for _, target := range targets {
		content, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("read %s: %v", target, err)
		}
		if got, want := string(content), "# Shared skill\n"; got != want {
			t.Fatalf("content at %s = %q, want %q", target, got, want)
		}
	}
}

func TestRunnerApplyAddRepeatAndUpdate(t *testing.T) {
	root := writePlanCatalogFixture(t)
	runner, stdout, stderr, agentHome := planRunner(t)
	catalogPath := filepath.Join(root, "agx.yaml")
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("lock code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath, "--json"}); code != ExitSuccess {
		t.Fatalf("apply code = %d, stderr = %q", code, stderr.String())
	}
	var firstResult applyResult
	if err := json.Unmarshal(stdout.Bytes(), &firstResult); err != nil {
		t.Fatal(err)
	}
	if !firstResult.Changed || firstResult.Summary.Add != 1 || firstResult.Generation == "" {
		t.Fatalf("apply result = %#v", firstResult)
	}
	targetPath := filepath.Join(agentHome, "skills", "code-review")
	sourceDigest, err := contenthash.Directory(filepath.Join(root, "skills", "code-review"))
	if err != nil {
		t.Fatal(err)
	}
	targetDigest, err := contenthash.Directory(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if targetDigest != sourceDigest {
		t.Fatalf("target digest = %q, want %q", targetDigest, sourceDigest)
	}
	firstGeneration, err := state.Current()
	if err != nil {
		t.Fatal(err)
	}
	if firstGeneration == nil || firstGeneration.ID != firstResult.Generation || len(firstGeneration.Entries) != 1 {
		t.Fatalf("current generation = %#v", firstGeneration)
	}
	assertNoApplyJournal(t)

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath, "--json"}); code != ExitSuccess {
		t.Fatalf("repeat apply code = %d, stderr = %q", code, stderr.String())
	}
	var repeated applyResult
	if err := json.Unmarshal(stdout.Bytes(), &repeated); err != nil {
		t.Fatal(err)
	}
	if repeated.Changed || repeated.Generation != firstGeneration.ID || repeated.Summary.Unchanged != 1 {
		t.Fatalf("repeat result = %#v", repeated)
	}

	if err := os.WriteFile(filepath.Join(root, "skills", "code-review", "SKILL.md"), []byte("# Updated review\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("updated lock code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath, "--json"}); code != ExitSuccess {
		t.Fatalf("updated apply code = %d, stderr = %q", code, stderr.String())
	}
	var updated applyResult
	if err := json.Unmarshal(stdout.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if !updated.Changed || updated.Summary.Update != 1 {
		t.Fatalf("updated result = %#v", updated)
	}
	updatedGeneration, err := state.Current()
	if err != nil {
		t.Fatal(err)
	}
	if updatedGeneration.PreviousID != firstGeneration.ID {
		t.Fatalf("previous generation = %q, want %q", updatedGeneration.PreviousID, firstGeneration.ID)
	}
	updatedTarget, err := os.ReadFile(filepath.Join(targetPath, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(updatedTarget) != "# Updated review\n" {
		t.Fatalf("updated target = %q", updatedTarget)
	}
}

func TestRunnerApplyRequiresExplicitAdopt(t *testing.T) {
	root := writePlanCatalogFixture(t)
	runner, stdout, stderr, agentHome := planRunner(t)
	catalogPath := filepath.Join(root, "agx.yaml")
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("lock code = %d", code)
	}
	targetRoot := filepath.Join(agentHome, "skills", "code-review")
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(root, "skills", "code-review", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetRoot, "SKILL.md"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath}); code != ExitTargetConflict {
		t.Fatalf("apply code = %d, want %d", code, ExitTargetConflict)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath, "--adopt", "--json"}); code != ExitSuccess {
		t.Fatalf("adopt apply code = %d, stderr = %q", code, stderr.String())
	}
	var result applyResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Summary.Adopt != 1 {
		t.Fatalf("adopt result = %#v", result)
	}
}

func TestRunnerApplyRejectsExternallyModifiedManagedTarget(t *testing.T) {
	root := writePlanCatalogFixture(t)
	runner, stdout, stderr, agentHome := planRunner(t)
	catalogPath := filepath.Join(root, "agx.yaml")
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("lock code = %d", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("apply code = %d, stderr = %q", code, stderr.String())
	}
	targetManifest := filepath.Join(agentHome, "skills", "code-review", "SKILL.md")
	if err := os.WriteFile(targetManifest, []byte("external change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath}); code != ExitTargetConflict {
		t.Fatalf("modified apply code = %d, want %d", code, ExitTargetConflict)
	}
	if !strings.Contains(stdout.String(), "managed target was modified outside AGX") {
		t.Fatalf("apply stdout = %q", stdout.String())
	}
}

func TestRunnerApplyRemovesPreviouslyManagedSkill(t *testing.T) {
	root := writeTwoSkillCatalogFixture(t)
	runner, stdout, stderr, agentHome := planRunner(t)
	catalogPath := filepath.Join(root, "agx.yaml")
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("lock code = %d", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("apply code = %d, stderr = %q", code, stderr.String())
	}
	if err := writeSingleSkillCatalog(root); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("second lock code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath, "--json"}); code != ExitSuccess {
		t.Fatalf("remove apply code = %d, stderr = %q", code, stderr.String())
	}
	var result applyResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Summary.Remove != 1 {
		t.Fatalf("remove result = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(agentHome, "skills", "second")); !os.IsNotExist(err) {
		t.Fatalf("removed target still exists; stat error = %v", err)
	}
	current, err := state.Current()
	if err != nil {
		t.Fatal(err)
	}
	if current == nil || len(current.Entries) != 1 || current.Entries[0].Skill != "personal/first" {
		t.Fatalf("current generation = %#v", current)
	}
}

func TestRunnerApplyRollsBackWhenStateWriteFails(t *testing.T) {
	root := writePlanCatalogFixture(t)
	runner, stdout, stderr, agentHome := planRunner(t)
	catalogPath := filepath.Join(root, "agx.yaml")
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("lock code = %d", code)
	}
	stateRoot := os.Getenv("AGX_STATE_HOME")
	if err := os.WriteFile(filepath.Join(stateRoot, "generations"), []byte("block directory creation"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath}); code != ExitFailure {
		t.Fatalf("apply code = %d, want %d", code, ExitFailure)
	}
	if !strings.Contains(stderr.String(), "AGX_STATE_WRITE_FAILED") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(agentHome, "skills", "code-review")); !os.IsNotExist(err) {
		t.Fatalf("target remains after rollback; stat error = %v", err)
	}
	assertNoApplyJournal(t)
}

func TestRunnerApplyRejectsUnfinishedTransaction(t *testing.T) {
	root := writePlanCatalogFixture(t)
	runner, _, stderr, _ := planRunner(t)
	catalogPath := filepath.Join(root, "agx.yaml")
	if err := installer.SaveJournal(installer.Journal{
		ID:    "transaction-interrupted",
		State: installer.StateApplying,
		Targets: []installer.TargetChange{{
			Agent:      "codex",
			Action:     "add",
			TargetPath: filepath.Join(root, "target"),
			StagePath:  filepath.Join(root, ".agx-stage-test", "content"),
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath}); code != ExitFailure {
		t.Fatalf("apply code = %d, want %d", code, ExitFailure)
	}
	if !strings.Contains(stderr.String(), "AGX_REPAIR_REQUIRED") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRollbackDeploymentsRestoresTargets(t *testing.T) {
	root := t.TempDir()
	added := filepath.Join(root, "added")
	if err := os.Mkdir(added, 0o755); err != nil {
		t.Fatal(err)
	}
	updated := filepath.Join(root, "updated")
	if err := os.Mkdir(updated, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(updated, "value"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	backupRoot := filepath.Join(root, "backup")
	backupPath := filepath.Join(backupRoot, "content")
	if err := os.MkdirAll(backupPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupPath, "value"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	deployments := []deployment{
		{change: planChange{Action: "add", Path: added}, installed: true},
		{change: planChange{Action: "update", Path: updated}, backupRoot: backupRoot, backupPath: backupPath, backedUp: true, installed: true},
	}
	if err := rollbackDeployments(deployments); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(added); !os.IsNotExist(err) {
		t.Fatalf("added target remains after rollback: %v", err)
	}
	value, err := os.ReadFile(filepath.Join(updated, "value"))
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != "old" {
		t.Fatalf("restored value = %q", value)
	}
}

func assertNoApplyJournal(t *testing.T) {
	t.Helper()
	journal, err := installer.LoadJournal()
	if err != nil {
		t.Fatal(err)
	}
	if journal != nil {
		t.Fatalf("transaction journal remains after apply: %#v", journal)
	}
}

func writeTwoSkillCatalogFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"first", "second"} {
		directory := filepath.Join(root, "skills", name)
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte("# "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
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
`
	if err := os.WriteFile(filepath.Join(root, "agx.yaml"), []byte(catalogYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeSingleSkillCatalog(root string) error {
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
`
	return os.WriteFile(filepath.Join(root, "agx.yaml"), []byte(catalogYAML), 0o644)
}
