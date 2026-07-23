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
	Summary    struct {
		Healthy int `json:"healthy"`
	} `json:"summary"`
}

type rollbackResult struct {
	From       string `json:"from"`
	Restored   string `json:"restored"`
	Generation string `json:"generation"`
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
	skillRoot := filepath.Join(catalogRoot, "skills", "review")
	if err := os.MkdirAll(skillRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(skillRoot, "SKILL.md")
	writeFile(t, manifest, "# Version one\n")
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
	stateHome := filepath.Join(workspace, "state")
	environment := overriddenEnvironment(map[string]string{
		"AGX_STATE_HOME": stateHome,
		"CODEX_HOME":     agentHome,
		"PATH":           binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
	})

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
	runAGX(t, binary, catalogRoot, environment, "lock", "--catalog", catalogPath)
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
