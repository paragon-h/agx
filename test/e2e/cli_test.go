package e2e_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type applyResult struct {
	Generation string `json:"generation"`
	Changed    bool   `json:"changed"`
}

type statusReport struct {
	State      string `json:"state"`
	Generation string `json:"generation"`
	Profile    string `json:"profile"`
	Summary    struct {
		Healthy int `json:"healthy"`
	} `json:"summary"`
}

func TestCLIEndToEndProfiles(t *testing.T) {
	repository := repositoryRoot(t)
	workspace := t.TempDir()
	binary := filepath.Join(workspace, executableName("agx"))
	runBuild(t, repository, binary)

	binDir := filepath.Join(workspace, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	copyExecutable(t, binary, filepath.Join(binDir, executableName("codex")))
	catalogRoot := filepath.Join(workspace, "catalog")
	writeFile(t, filepath.Join(catalogRoot, "skills", "first", "SKILL.md"), "# First\n")
	writeFile(t, filepath.Join(catalogRoot, "skills", "second", "SKILL.md"), "# Second\n")
	writeFile(t, filepath.Join(catalogRoot, "agx.yaml"), `apiVersion: agx.dev/v1alpha1
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
`)
	agentHome := filepath.Join(workspace, "codex-home")
	environment := overriddenEnvironment(map[string]string{
		"AGX_STATE_HOME": filepath.Join(workspace, "state"),
		"AGX_STORE_HOME": filepath.Join(workspace, "store"),
		"CODEX_HOME":     agentHome,
		"PATH":           binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
	})

	runAGX(t, binary, catalogRoot, environment, "lock")
	listed := runAGX(t, binary, catalogRoot, environment, "list", "--profile", "first-only")
	if listed != "personal/first\tlocal\tcodex\n" {
		t.Fatalf("profile list = %q", listed)
	}
	plan := runAGX(t, binary, catalogRoot, environment, "plan", "--profile", "first-only")
	if !strings.Contains(plan, "profile: first-only") || !strings.Contains(plan, "personal/first") || strings.Contains(plan, "personal/second") {
		t.Fatalf("profile plan = %q", plan)
	}
	var applied applyResult
	decodeJSON(t, runAGX(t, binary, catalogRoot, environment, "apply", "--profile", "first-only", "--json"), &applied)
	if !applied.Changed {
		t.Fatalf("profile apply = %#v", applied)
	}
	if _, err := os.Stat(filepath.Join(agentHome, "skills", "first", "SKILL.md")); err != nil {
		t.Fatalf("selected Skill: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentHome, "skills", "second")); !os.IsNotExist(err) {
		t.Fatalf("unselected Skill exists: %v", err)
	}
	var status statusReport
	decodeJSON(t, runAGX(t, binary, catalogRoot, environment, "status", "--json"), &status)
	if status.State != "healthy" || status.Profile != "first-only" {
		t.Fatalf("profile status = %#v", status)
	}
}

type rollbackResult struct {
	From       string `json:"from"`
	Restored   string `json:"restored"`
	Generation string `json:"generation"`
}

type storeStatusResult struct {
	References int `json:"references"`
	Summary    struct {
		Objects      int `json:"objects"`
		Referenced   int `json:"referenced"`
		Unreferenced int `json:"unreferenced"`
	} `json:"summary"`
}

func TestCLIEndToEndActiveCatalogDiscovery(t *testing.T) {
	repository := repositoryRoot(t)
	workspace := t.TempDir()
	binary := filepath.Join(workspace, executableName("agx"))
	runBuild(t, repository, binary)

	catalogRoot := filepath.Join(workspace, "catalog")
	catalogPath := filepath.Join(catalogRoot, "agx.yaml")
	writeFile(t, filepath.Join(catalogRoot, "skills", "review", "SKILL.md"), "# Review\n")
	writeFile(t, catalogPath, `apiVersion: agx.dev/v1alpha1
kind: Catalog
metadata:
  name: personal
skills:
  review:
    source:
      type: local
      path: skills/review
    targets:
      codex: {}
`)
	environment := overriddenEnvironment(map[string]string{
		"AGX_CONFIG_HOME": filepath.Join(workspace, "config"),
		"AGX_STORE_HOME":  filepath.Join(workspace, "store"),
	})

	runAGX(t, binary, catalogRoot, environment, "catalog", "add", "personal", "--path", ".")
	awayFromCatalog := filepath.Join(workspace, "empty")
	if err := os.MkdirAll(awayFromCatalog, 0o755); err != nil {
		t.Fatal(err)
	}
	listed := runAGX(t, binary, awayFromCatalog, environment, "list")
	if listed != "personal/review\tlocal\tcodex\n" {
		t.Fatalf("active catalog list = %q", listed)
	}
	runAGX(t, binary, awayFromCatalog, environment, "lock")
	if _, err := os.Stat(filepath.Join(catalogRoot, "agx.lock")); err != nil {
		t.Fatalf("active catalog lockfile: %v", err)
	}
}

func TestCLIEndToEndAppliesStoredContentWithoutSource(t *testing.T) {
	repository := repositoryRoot(t)
	workspace := t.TempDir()
	binary := filepath.Join(workspace, executableName("agx"))
	runBuild(t, repository, binary)

	binDir := filepath.Join(workspace, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	copyExecutable(t, binary, filepath.Join(binDir, executableName("codex")))
	catalogRoot := filepath.Join(workspace, "catalog")
	skillRoot := filepath.Join(catalogRoot, "skills", "review")
	writeFile(t, filepath.Join(skillRoot, "SKILL.md"), "# Stored review\n")
	catalogPath := filepath.Join(catalogRoot, "agx.yaml")
	writeFile(t, catalogPath, `apiVersion: agx.dev/v1alpha1
kind: Catalog
metadata:
  name: personal
skills:
  review:
    source:
      type: local
      path: skills/review
    targets:
      codex: {}
`)
	agentHome := filepath.Join(workspace, "codex-home")
	environment := overriddenEnvironment(map[string]string{
		"AGX_STATE_HOME": filepath.Join(workspace, "state"),
		"AGX_STORE_HOME": filepath.Join(workspace, "store"),
		"CODEX_HOME":     agentHome,
		"PATH":           binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
	})

	runAGX(t, binary, catalogRoot, environment, "lock")
	if err := os.RemoveAll(skillRoot); err != nil {
		t.Fatal(err)
	}
	runAGX(t, binary, catalogRoot, environment, "plan")
	runAGX(t, binary, catalogRoot, environment, "apply")
	assertFileContent(t, filepath.Join(agentHome, "skills", "review", "SKILL.md"), "# Stored review\n")
	var initialStore storeStatusResult
	decodeJSON(t, runAGX(t, binary, catalogRoot, environment, "store", "status", "--json"), &initialStore)
	if initialStore.References != 1 || initialStore.Summary.Objects != 1 || initialStore.Summary.Referenced != 1 || initialStore.Summary.Unreferenced != 0 {
		t.Fatalf("initial Store status = %#v", initialStore)
	}
	runAGX(t, binary, catalogRoot, environment, "store", "verify")

	writeFile(t, filepath.Join(skillRoot, "SKILL.md"), "# Candidate review\n")
	runAGX(t, binary, catalogRoot, environment, "update", "--check", "--json")
	var candidateStore storeStatusResult
	decodeJSON(t, runAGX(t, binary, catalogRoot, environment, "store", "status", "--json"), &candidateStore)
	if candidateStore.Summary.Objects != 2 || candidateStore.Summary.Referenced != 1 || candidateStore.Summary.Unreferenced != 1 {
		t.Fatalf("candidate Store status = %#v", candidateStore)
	}
	runAGX(t, binary, catalogRoot, environment, "store", "gc", "--dry-run")
	runAGX(t, binary, catalogRoot, environment, "store", "gc")
	var collectedStore storeStatusResult
	decodeJSON(t, runAGX(t, binary, catalogRoot, environment, "store", "status", "--json"), &collectedStore)
	if collectedStore.Summary.Objects != 1 || collectedStore.Summary.Referenced != 1 || collectedStore.Summary.Unreferenced != 0 {
		t.Fatalf("collected Store status = %#v", collectedStore)
	}
}

func TestCLIEndToEndApplyStatusAndRollback(t *testing.T) {
	repository := repositoryRoot(t)
	workspace := t.TempDir()
	binary := filepath.Join(workspace, executableName("agx"))
	runBuild(t, repository, binary)

	binDir := filepath.Join(workspace, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	copyExecutable(t, binary, filepath.Join(binDir, executableName("codex")))
	catalogRoot := filepath.Join(workspace, "catalog")
	catalogPath := filepath.Join(catalogRoot, "agx.yaml")
	agentHome := filepath.Join(workspace, "codex-home")
	stateHome := filepath.Join(workspace, "state")
	environment := overriddenEnvironment(map[string]string{
		"AGX_STATE_HOME": stateHome,
		"CODEX_HOME":     agentHome,
		"PATH":           binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
	})

	runAGX(t, binary, workspace, environment, "init", "--catalog", catalogPath, "--name", "personal")
	skillRoot := filepath.Join(catalogRoot, "skills", "review")
	manifest := filepath.Join(skillRoot, "SKILL.md")
	writeFile(t, manifest, "# Version one\n")
	writeFile(t, catalogPath, `apiVersion: agx.dev/v1alpha1
kind: Catalog
metadata:
  name: personal
skills:
  review:
    source:
      type: local
      path: skills/review
    targets:
      codex: {}
`)

	runAGX(t, binary, catalogRoot, environment, "lock", "--catalog", catalogPath)
	runAGX(t, binary, catalogRoot, environment, "audit", "review", "--catalog", catalogPath, "--json")
	runAGX(t, binary, catalogRoot, environment, "approve", "review", "--catalog", catalogPath, "--json")
	plan := runAGX(t, binary, catalogRoot, environment, "plan", "--catalog", catalogPath, "--json")
	if !strings.Contains(plan, `"action":"add"`) {
		t.Fatalf("initial plan = %s", plan)
	}
	firstOutput := runAGX(t, binary, catalogRoot, environment, "apply", "--catalog", catalogPath, "--json")
	var first applyResult
	decodeJSON(t, firstOutput, &first)
	if !first.Changed || first.Generation == "" {
		t.Fatalf("first apply = %#v", first)
	}
	assertHealthyStatus(t, runAGX(t, binary, catalogRoot, environment, "status", "--json"), first.Generation)

	writeFile(t, manifest, "# Version two\n")
	updateCheck := runAGX(t, binary, catalogRoot, environment, "update", "--check", "--catalog", catalogPath, "--json")
	if !strings.Contains(updateCheck, `"changed":1`) {
		t.Fatalf("update check = %s", updateCheck)
	}
	runAGX(t, binary, catalogRoot, environment, "update", "review", "--accept", "--catalog", catalogPath, "--json")
	secondOutput := runAGX(t, binary, catalogRoot, environment, "apply", "--catalog", catalogPath, "--json")
	var second applyResult
	decodeJSON(t, secondOutput, &second)
	if !second.Changed || second.Generation == first.Generation {
		t.Fatalf("second apply = %#v", second)
	}

	rollbackOutput := runAGX(t, binary, catalogRoot, environment, "rollback", "--json")
	var rollback rollbackResult
	decodeJSON(t, rollbackOutput, &rollback)
	if rollback.From != second.Generation || rollback.Restored != first.Generation || rollback.Generation == "" {
		t.Fatalf("rollback = %#v", rollback)
	}
	installed, err := os.ReadFile(filepath.Join(agentHome, "skills", "review", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(installed) != "# Version one\n" {
		t.Fatalf("installed manifest after rollback = %q", installed)
	}
	assertHealthyStatus(t, runAGX(t, binary, catalogRoot, environment, "status", "--json"), rollback.Generation)
}

func TestCLIEndToEndOverlayUpdateAndRollback(t *testing.T) {
	repository := repositoryRoot(t)
	workspace := t.TempDir()
	binary := filepath.Join(workspace, executableName("agx"))
	runBuild(t, repository, binary)

	binDir := filepath.Join(workspace, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	copyExecutable(t, binary, filepath.Join(binDir, executableName("codex")))
	catalogRoot := filepath.Join(workspace, "catalog")
	skillRoot := filepath.Join(catalogRoot, "skills", "review")
	writeFile(t, filepath.Join(skillRoot, "SKILL.md"), "# Review\n")
	writeFile(t, filepath.Join(skillRoot, "scripts", "upload.sh"), "#!/bin/sh\n")
	overlayRoot := filepath.Join(catalogRoot, "overlays", "review")
	writeFile(t, filepath.Join(overlayRoot, "overlay.yaml"), `apiVersion: agx.dev/v1alpha1
kind: Overlay
content:
  prepend: prepend.md
  append: append.md
disableScripts:
  - scripts/upload.sh
`)
	prependPath := filepath.Join(overlayRoot, "prepend.md")
	writeFile(t, prependPath, "Policy one\n")
	writeFile(t, filepath.Join(overlayRoot, "append.md"), "Footer\n")
	catalogPath := filepath.Join(catalogRoot, "agx.yaml")
	writeFile(t, catalogPath, `apiVersion: agx.dev/v1alpha1
kind: Catalog
metadata:
  name: personal
skills:
  review:
    source:
      type: local
      path: skills/review
    overlay: overlays/review
    targets:
      codex: {}
`)

	agentHome := filepath.Join(workspace, "codex-home")
	environment := overriddenEnvironment(map[string]string{
		"AGX_STATE_HOME": filepath.Join(workspace, "state"),
		"CODEX_HOME":     agentHome,
		"PATH":           binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
	})

	runAGX(t, binary, catalogRoot, environment, "lock", "--catalog", catalogPath)
	runAGX(t, binary, catalogRoot, environment, "audit", "review", "--catalog", catalogPath, "--json")
	runAGX(t, binary, catalogRoot, environment, "approve", "review", "--catalog", catalogPath, "--json")
	plan := runAGX(t, binary, catalogRoot, environment, "plan", "--catalog", catalogPath, "--json")
	if !strings.Contains(plan, `"action":"add"`) {
		t.Fatalf("overlay plan = %s", plan)
	}
	firstOutput := runAGX(t, binary, catalogRoot, environment, "apply", "--catalog", catalogPath, "--json")
	var first applyResult
	decodeJSON(t, firstOutput, &first)
	if !first.Changed || first.Generation == "" {
		t.Fatalf("first overlay apply = %#v", first)
	}
	assertHealthyStatus(t, runAGX(t, binary, catalogRoot, environment, "status", "--json"), first.Generation)
	installedRoot := filepath.Join(agentHome, "skills", "review")
	assertFileContent(t, filepath.Join(installedRoot, "SKILL.md"), "Policy one\n# Review\nFooter\n")
	if _, err := os.Lstat(filepath.Join(installedRoot, "scripts", "upload.sh")); !os.IsNotExist(err) {
		t.Fatalf("disabled script still exists: %v", err)
	}

	writeFile(t, prependPath, "Policy two\n")
	runAGX(t, binary, catalogRoot, environment, "lock", "--catalog", catalogPath)
	secondOutput := runAGX(t, binary, catalogRoot, environment, "apply", "--catalog", catalogPath, "--json")
	var second applyResult
	decodeJSON(t, secondOutput, &second)
	if !second.Changed || second.Generation == first.Generation {
		t.Fatalf("second overlay apply = %#v", second)
	}
	assertFileContent(t, filepath.Join(installedRoot, "SKILL.md"), "Policy two\n# Review\nFooter\n")

	rollbackOutput := runAGX(t, binary, catalogRoot, environment, "rollback", "--json")
	var rollback rollbackResult
	decodeJSON(t, rollbackOutput, &rollback)
	if rollback.From != second.Generation || rollback.Restored != first.Generation || rollback.Generation == "" {
		t.Fatalf("overlay rollback = %#v", rollback)
	}
	assertFileContent(t, filepath.Join(installedRoot, "SKILL.md"), "Policy one\n# Review\nFooter\n")
	if _, err := os.Lstat(filepath.Join(installedRoot, "scripts", "upload.sh")); !os.IsNotExist(err) {
		t.Fatalf("disabled script returned after rollback: %v", err)
	}
	assertHealthyStatus(t, runAGX(t, binary, catalogRoot, environment, "status", "--json"), rollback.Generation)
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func executableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func overriddenEnvironment(overrides map[string]string) []string {
	result := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		key, _, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		overridden := false
		for override := range overrides {
			if key == override || (runtime.GOOS == "windows" && strings.EqualFold(key, override)) {
				overridden = true
				break
			}
		}
		if !overridden {
			result = append(result, entry)
		}
	}
	for key, value := range overrides {
		result = append(result, key+"="+value)
	}
	return result
}

func runBuild(t *testing.T, repository, output string) {
	t.Helper()
	command := exec.Command("go", "build", "-o", output, "./cmd/agx")
	command.Dir = repository
	if combined, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build agx: %v\n%s", err, combined)
	}
}

func copyExecutable(t *testing.T, source, destination string) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, data, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, data, want)
	}
}

func runAGX(t *testing.T, binary, directory string, environment []string, args ...string) string {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Dir = directory
	command.Env = environment
	combined, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("agx %s: %v\n%s", strings.Join(args, " "), err, combined)
	}
	return string(combined)
}

func decodeJSON(t *testing.T, value string, destination any) {
	t.Helper()
	if err := json.Unmarshal([]byte(value), destination); err != nil {
		t.Fatalf("decode JSON %q: %v", value, err)
	}
}

func assertHealthyStatus(t *testing.T, output, generation string) {
	t.Helper()
	var report statusReport
	decodeJSON(t, output, &report)
	if report.State != "healthy" || report.Generation != generation || report.Summary.Healthy != 1 {
		t.Fatalf("status = %s; want healthy generation %s", fmt.Sprintf("%#v", report), generation)
	}
}
