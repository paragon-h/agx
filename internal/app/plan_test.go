package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paragon-h/agx/internal/lockfile"
	"github.com/paragon-h/agx/internal/store"
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

func TestRunnerAppliesStoredLocalSkillAndOverlayWhenSourcesAreUnavailable(t *testing.T) {
	root := writePlanCatalogFixture(t)
	skillRoot := filepath.Join(root, "skills", "code-review")
	overlayRoot := filepath.Join(root, "overlays", "code-review")
	if err := os.MkdirAll(overlayRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overlayRoot, "overlay.yaml"), []byte(`apiVersion: agx.dev/v1alpha1
kind: Overlay
content:
  prepend: prepend.md
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overlayRoot, "prepend.md"), []byte("Stored policy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(root, "agx.yaml")
	if err := os.WriteFile(catalogPath, []byte(`apiVersion: agx.dev/v1alpha1
kind: Catalog
metadata:
  name: personal
skills:
  code-review:
    source:
      type: local
      path: skills/code-review
    overlay: overlays/code-review
    targets:
      codex: {}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	runner, stdout, stderr, agentHome := planRunner(t)
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("lock code = %d, stderr = %q", code, stderr.String())
	}
	locked, err := lockfile.Load(filepath.Join(root, "agx.lock"))
	if err != nil {
		t.Fatal(err)
	}
	lockedSkill := locked.Skills["code-review"]
	if err := store.Verify(lockedSkill.ContentDigest); err != nil {
		t.Fatalf("source Store object: %v", err)
	}
	if err := store.Verify(lockedSkill.OverlayDigest); err != nil {
		t.Fatalf("Overlay Store object: %v", err)
	}
	if err := os.RemoveAll(skillRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(overlayRoot); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"plan", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("stored plan code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("stored apply code = %d, stderr = %q", code, stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(agentHome, "skills", "code-review", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "Stored policy\n# Code review\n"; got != want {
		t.Fatalf("installed stored Skill = %q, want %q", got, want)
	}
}

func TestRunnerRejectsCorruptStoredSkill(t *testing.T) {
	root := writePlanCatalogFixture(t)
	runner, _, stderr, _ := planRunner(t)
	catalogPath := filepath.Join(root, "agx.yaml")
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("lock code = %d, stderr = %q", code, stderr.String())
	}
	locked, err := lockfile.Load(filepath.Join(root, "agx.lock"))
	if err != nil {
		t.Fatal(err)
	}
	objectPath, err := store.ObjectPath(locked.Skills["code-review"].ContentDigest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(objectPath, "SKILL.md"), []byte("corrupt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"plan", "--catalog", catalogPath}); code != ExitSourceFailure {
		t.Fatalf("corrupt Store plan code = %d, want %d; stderr = %q", code, ExitSourceFailure, stderr.String())
	}
	if !strings.Contains(stderr.String(), "Store object") || !strings.Contains(stderr.String(), "corrupt") {
		t.Fatalf("corrupt Store stderr = %q", stderr.String())
	}
}

func TestRunnerAppliesStoredGitSkillWithoutRepository(t *testing.T) {
	t.Setenv("AGX_STORE_HOME", t.TempDir())
	repository := t.TempDir()
	runGitCommand(t, repository, "init", "--quiet", "--initial-branch=main")
	runGitCommand(t, repository, "config", "user.name", "AGX Test")
	runGitCommand(t, repository, "config", "user.email", "agx@example.invalid")
	skillRoot := filepath.Join(repository, "skills", "review")
	if err := os.MkdirAll(skillRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte("# Offline review\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, repository, "add", ".")
	runGitCommand(t, repository, "commit", "--quiet", "-m", "add offline skill")

	catalogRoot := t.TempDir()
	catalogPath := filepath.Join(catalogRoot, "agx.yaml")
	catalogYAML := fmt.Sprintf(`apiVersion: agx.dev/v1alpha1
kind: Catalog
metadata:
  name: personal
skills:
  review:
    source:
      type: git
      repository: %q
      revision: main
      path: skills/review
    targets:
      codex: {}
`, repository)
	if err := os.WriteFile(catalogPath, []byte(catalogYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	runner, stdout, stderr, agentHome := planRunner(t)
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("lock code = %d, stderr = %q", code, stderr.String())
	}
	if err := os.RemoveAll(repository); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"approve", "review", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("stored approve code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"plan", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("stored Git plan code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("stored Git apply code = %d, stderr = %q", code, stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(agentHome, "skills", "review", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# Offline review\n" {
		t.Fatalf("installed stored Git Skill = %q", data)
	}
}

func TestRunnerAppliesLockedOverlay(t *testing.T) {
	root := writePlanCatalogFixture(t)
	skillRoot := filepath.Join(root, "skills", "code-review")
	if err := os.MkdirAll(filepath.Join(skillRoot, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "scripts", "upload.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	overlayRoot := filepath.Join(root, "overlays", "code-review")
	if err := os.MkdirAll(overlayRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overlayRoot, "overlay.yaml"), []byte(`apiVersion: agx.dev/v1alpha1
kind: Overlay
content:
  prepend: prepend.md
disableScripts:
  - scripts/upload.sh
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overlayRoot, "prepend.md"), []byte("Local policy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(root, "agx.yaml")
	catalogYAML := `apiVersion: agx.dev/v1alpha1
kind: Catalog
metadata:
  name: personal
skills:
  code-review:
    source:
      type: local
      path: skills/code-review
    overlay: overlays/code-review
    targets:
      codex: {}
`
	if err := os.WriteFile(catalogPath, []byte(catalogYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	runner, stdout, stderr, agentHome := planRunner(t)
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("lock code = %d, stderr = %q", code, stderr.String())
	}
	locked, err := lockfile.Load(filepath.Join(root, "agx.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if locked.Skills["code-review"].OverlayDigest == "" {
		t.Fatal("overlay digest was not locked")
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
	if len(report.Changes) != 1 || report.Changes[0].DesiredDigest == locked.Skills["code-review"].ContentDigest {
		t.Fatalf("plan did not use rendered overlay digest: %#v", report)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"apply", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("apply code = %d, stderr = %q", code, stderr.String())
	}
	installedRoot := filepath.Join(agentHome, "skills", "code-review")
	data, err := os.ReadFile(filepath.Join(installedRoot, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "Local policy\n# Code review\n"; got != want {
		t.Fatalf("installed SKILL.md = %q, want %q", got, want)
	}
	if _, err := os.Lstat(filepath.Join(installedRoot, "scripts", "upload.sh")); !os.IsNotExist(err) {
		t.Fatalf("disabled script still exists: %v", err)
	}

	if err := os.WriteFile(filepath.Join(overlayRoot, "prepend.md"), []byte("Changed policy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath, "--frozen"}); code != ExitLockOutdated {
		t.Fatalf("changed overlay frozen lock code = %d, want %d; stderr = %q", code, ExitLockOutdated, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"audit", "code-review", "--catalog", catalogPath}); code != ExitLockOutdated {
		t.Fatalf("changed overlay audit code = %d, want %d; stderr = %q", code, ExitLockOutdated, stderr.String())
	}
}

func TestRunnerLockRejectsUnusableOverlayWithoutReplacingLockfile(t *testing.T) {
	root := writePlanCatalogFixture(t)
	overlayRoot := filepath.Join(root, "overlays", "code-review")
	if err := os.MkdirAll(overlayRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overlayRoot, "overlay.yaml"), []byte(`apiVersion: agx.dev/v1alpha1
kind: Overlay
content:
  prepend: missing.md
`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(root, "agx.yaml")
	if err := os.WriteFile(catalogPath, []byte(`apiVersion: agx.dev/v1alpha1
kind: Catalog
metadata:
  name: personal
skills:
  code-review:
    source:
      type: local
      path: skills/code-review
    overlay: overlays/code-review
    targets:
      codex: {}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	runner, _, stderr, _ := planRunner(t)
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSourceFailure {
		t.Fatalf("invalid overlay lock code = %d, want %d; stderr = %q", code, ExitSourceFailure, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, "agx.lock")); !os.IsNotExist(err) {
		t.Fatalf("invalid overlay created lockfile: %v", err)
	}

	if err := os.WriteFile(filepath.Join(overlayRoot, "missing.md"), []byte("Policy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("valid overlay lock code = %d, stderr = %q", code, stderr.String())
	}
	lockPath := filepath.Join(root, "agx.lock")
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overlayRoot, "overlay.yaml"), []byte(`apiVersion: agx.dev/v1alpha1
kind: Overlay
content:
  prepend: missing.md
disableScripts:
  - scripts/missing.sh
`), 0o644); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSourceFailure {
		t.Fatalf("unapplicable overlay lock code = %d, want %d; stderr = %q", code, ExitSourceFailure, stderr.String())
	}
	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("unapplicable overlay lock replaced the existing lockfile")
	}
	if err := os.WriteFile(filepath.Join(overlayRoot, "overlay.yaml"), []byte(`apiVersion: agx.dev/v1alpha1
kind: Overlay
content:
  prepend: missing.md
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(overlayRoot, "missing.md")); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSourceFailure {
		t.Fatalf("missing overlay content lock code = %d, want %d; stderr = %q", code, ExitSourceFailure, stderr.String())
	}
	after, err = os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("failed overlay lock replaced the existing lockfile")
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
	t.Setenv("AGX_STATE_HOME", t.TempDir())
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
	t.Setenv("AGX_STORE_HOME", t.TempDir())
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
	t.Setenv("AGX_STATE_HOME", t.TempDir())
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	return New(stdout, stderr, "dev"), stdout, stderr, agentHome
}
