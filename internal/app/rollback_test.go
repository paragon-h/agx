package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alanhuangch/agx/internal/state"
)

const testGenerationDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestRunnerRollbackRestoresPreviousGeneration(t *testing.T) {
	root := writePlanCatalogFixture(t)
	runner, stdout, stderr, agentHome := planRunner(t)
	catalogPath := filepath.Join(root, "agx.yaml")
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("first lock code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("first apply code = %d, stderr = %q", code, stderr.String())
	}
	first, err := state.Current()
	if err != nil || first == nil {
		t.Fatalf("first generation = %#v, err = %v", first, err)
	}
	if first.Entries[0].Artifact == "" {
		t.Fatal("first generation has no rollback artifact")
	}
	manifest := filepath.Join(root, "skills", "code-review", "SKILL.md")
	if err := os.WriteFile(manifest, []byte("# Version two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("second lock code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("second apply code = %d, stderr = %q", code, stderr.String())
	}
	second, err := state.Current()
	if err != nil || second == nil || second.PreviousID != first.ID {
		t.Fatalf("second generation = %#v, err = %v", second, err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"rollback", "--json"}); code != ExitSuccess {
		t.Fatalf("rollback code = %d, stderr = %q", code, stderr.String())
	}
	var result rollbackResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.From != second.ID || result.Restored != first.ID || result.Generation == "" || result.Summary.Update != 1 {
		t.Fatalf("rollback result = %#v", result)
	}
	installed, err := os.ReadFile(filepath.Join(agentHome, "skills", "code-review", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(installed) != "# Code review\n" {
		t.Fatalf("restored manifest = %q", installed)
	}
	rolledBack, err := state.Current()
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.ID != result.Generation || rolledBack.PreviousID != second.ID || rolledBack.Entries[0].ContentDigest != first.Entries[0].ContentDigest {
		t.Fatalf("rollback generation = %#v", rolledBack)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"status", "--json"}); code != ExitSuccess {
		t.Fatalf("status code = %d, stderr = %q", code, stderr.String())
	}
}

func TestRunnerRollbackRejectsExternalModification(t *testing.T) {
	root := writePlanCatalogFixture(t)
	runner, stdout, stderr, agentHome := planRunner(t)
	catalogPath := filepath.Join(root, "agx.yaml")
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatal(stderr.String())
	}
	stdout.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatal(stderr.String())
	}
	first, _ := state.Current()
	if err := os.WriteFile(filepath.Join(root, "skills", "code-review", "SKILL.md"), []byte("# Version two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatal(stderr.String())
	}
	stdout.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatal(stderr.String())
	}
	if err := os.WriteFile(filepath.Join(agentHome, "skills", "code-review", "SKILL.md"), []byte("external\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"rollback", "--generation", first.ID}); code != ExitTargetConflict {
		t.Fatalf("rollback code = %d, want %d", code, ExitTargetConflict)
	}
	if !strings.Contains(stdout.String(), "managed target was modified outside AGX") {
		t.Fatalf("rollback stdout = %q", stdout.String())
	}
}

func TestRunnerRollbackReaddsRemovedSkill(t *testing.T) {
	root := writeTwoSkillCatalogFixture(t)
	runner, stdout, stderr, agentHome := planRunner(t)
	catalogPath := filepath.Join(root, "agx.yaml")
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatal(stderr.String())
	}
	stdout.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatal(stderr.String())
	}
	first, _ := state.Current()
	if err := writeSingleSkillCatalog(root); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatal(stderr.String())
	}
	stdout.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatal(stderr.String())
	}
	removed := filepath.Join(agentHome, "skills", "second")
	if _, err := os.Stat(removed); !os.IsNotExist(err) {
		t.Fatalf("second skill was not removed: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"rollback", "--generation", first.ID, "--json"}); code != ExitSuccess {
		t.Fatalf("rollback code = %d, stderr = %q", code, stderr.String())
	}
	var result rollbackResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Summary.Add != 1 {
		t.Fatalf("rollback result = %#v", result)
	}
	content, err := os.ReadFile(filepath.Join(removed, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "# second\n" {
		t.Fatalf("restored second skill = %q", content)
	}
}

func TestRunnerRollbackRejectsGenerationWithoutArtifacts(t *testing.T) {
	runner, _, stderr, _ := planRunner(t)
	stateRoot := os.Getenv("AGX_STATE_HOME")
	targetPath := filepath.Join(stateRoot, "managed", "review")
	legacy := state.Generation{
		ID:             "legacy-generation",
		CreatedAt:      "2026-07-23T10:00:00Z",
		CatalogDigest:  testGenerationDigest,
		LockfileDigest: testGenerationDigest,
		Entries: []state.Entry{{
			Target:        "codex",
			Skill:         "personal/review",
			Path:          targetPath,
			ContentDigest: testGenerationDigest,
		}},
	}
	if err := state.Save(legacy); err != nil {
		t.Fatal(err)
	}
	current := legacy
	current.ID = "current-generation"
	current.PreviousID = legacy.ID
	if err := state.Save(current); err != nil {
		t.Fatal(err)
	}
	if code := runner.Run(context.Background(), []string{"rollback"}); code != ExitFailure {
		t.Fatalf("rollback code = %d, want %d", code, ExitFailure)
	}
	if !strings.Contains(stderr.String(), "AGX_GENERATION_CONTENT_INVALID") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunnerRollbackRequiresPreviousGeneration(t *testing.T) {
	root := writePlanCatalogFixture(t)
	runner, stdout, stderr, _ := planRunner(t)
	catalogPath := filepath.Join(root, "agx.yaml")
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatal(stderr.String())
	}
	stdout.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatal(stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"rollback"}); code != ExitFailure {
		t.Fatalf("rollback code = %d, want %d", code, ExitFailure)
	}
	if !strings.Contains(stderr.String(), "AGX_NO_PREVIOUS_GENERATION") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
