package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paragon-h/agx/internal/mcpconfig"
	"github.com/paragon-h/agx/internal/state"
)

func TestRunnerManagesCodexMCPServersAndPreservesUserConfig(t *testing.T) {
	root := t.TempDir()
	serverExecutable := filepath.Join(root, "github-mcp-server")
	writeTestExecutable(t, serverExecutable)
	catalogPath := filepath.Join(root, "agx.yaml")
	writeMCPCatalog(t, catalogPath, serverExecutable, []string{"stdio"}, true)
	t.Setenv("GITHUB_TOKEN", "secret-value-must-not-be-stored")
	runner, stdout, stderr, codexHome := planRunner(t)
	target := filepath.Join(codexHome, "config.toml")
	userConfig := "model = \"gpt-5\"\n\n[mcp_servers.personal]\ncommand = \"personal-server\"\n"
	if err := os.WriteFile(target, []byte(userConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	runMCPCommand(t, runner, stdout, stderr, "lock", "--catalog", catalogPath)
	lockContent, err := os.ReadFile(filepath.Join(root, "agx.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(lockContent), "secret-value-must-not-be-stored") {
		t.Fatalf("lockfile contains secret value: %s", lockContent)
	}
	runMCPCommand(t, runner, stdout, stderr, "apply", "--catalog", catalogPath)
	first, err := state.Current()
	if err != nil || first == nil || len(first.Entries) != 1 {
		t.Fatalf("first generation = %#v, err = %v", first, err)
	}
	if entry := first.Entries[0]; entry.Skill != "mcp-servers" || entry.Kind != "file" || entry.ManagedDigest == "" {
		t.Fatalf("MCP generation entry = %#v", first.Entries[0])
	}
	assertMCPConfig(t, target, "model = \"gpt-5\"", "[mcp_servers.personal]", "[mcp_servers.\"github\"]", `env_vars = ["GITHUB_TOKEN"]`)
	deployed, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(deployed), "secret-value-must-not-be-stored") {
		t.Fatalf("Codex config contains secret value: %s", deployed)
	}

	changedUserConfig := strings.Replace(string(deployed), "model = \"gpt-5\"", "model = \"gpt-5.1\"", 1)
	if err := os.WriteFile(target, []byte(changedUserConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	runMCPCommand(t, runner, stdout, stderr, "status")

	writeMCPCatalog(t, catalogPath, serverExecutable, []string{"stdio", "--read-only"}, true)
	runMCPCommand(t, runner, stdout, stderr, "lock", "--catalog", catalogPath)
	runMCPCommand(t, runner, stdout, stderr, "apply", "--catalog", catalogPath)
	second, err := state.Current()
	if err != nil || second == nil || second.PreviousID != first.ID {
		t.Fatalf("second generation = %#v, err = %v", second, err)
	}
	assertMCPConfig(t, target, "model = \"gpt-5.1\"", "--read-only")

	runMCPCommand(t, runner, stdout, stderr, "rollback", "--generation", first.ID)
	rolledBack, err := state.Current()
	if err != nil || rolledBack == nil {
		t.Fatalf("rollback generation = %#v, err = %v", rolledBack, err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "model = \"gpt-5.1\"") || strings.Contains(string(content), "--read-only") {
		t.Fatalf("rolled back Codex config = %s", content)
	}

	tampered := strings.Replace(string(content), filepath.ToSlash(serverExecutable), "tampered-server", 1)
	if tampered == string(content) {
		tampered = strings.Replace(string(content), serverExecutable, "tampered-server", 1)
	}
	if err := os.WriteFile(target, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"status"}); code != ExitTargetConflict {
		t.Fatalf("drifted status code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	if err := os.WriteFile(target, content, 0o644); err != nil {
		t.Fatal(err)
	}

	writeMCPCatalog(t, catalogPath, serverExecutable, nil, false)
	runMCPCommand(t, runner, stdout, stderr, "lock", "--catalog", catalogPath)
	runMCPCommand(t, runner, stdout, stderr, "apply", "--catalog", catalogPath, "--allow-empty")
	released, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(released), mcpconfig.BeginMarker) || !strings.Contains(string(released), "model = \"gpt-5.1\"") || !strings.Contains(string(released), "[mcp_servers.personal]") {
		t.Fatalf("released Codex config = %s", released)
	}
	runMCPCommand(t, runner, stdout, stderr, "rollback", "--generation", rolledBack.ID)
	assertMCPConfig(t, target, "model = \"gpt-5.1\"", "[mcp_servers.\"github\"]")
}

func TestRunnerRejectsUnmanagedMCPNameCollision(t *testing.T) {
	root := t.TempDir()
	serverExecutable := filepath.Join(root, "github-mcp-server")
	writeTestExecutable(t, serverExecutable)
	catalogPath := filepath.Join(root, "agx.yaml")
	writeMCPCatalog(t, catalogPath, serverExecutable, []string{"stdio"}, true)
	t.Setenv("GITHUB_TOKEN", "token")
	runner, stdout, stderr, codexHome := planRunner(t)
	config := "[mcp_servers.github]\ncommand = \"another-server\"\n"
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	runMCPCommand(t, runner, stdout, stderr, "lock", "--catalog", catalogPath)
	for _, args := range [][]string{
		{"plan", "--catalog", catalogPath},
		{"plan", "--catalog", catalogPath, "--adopt"},
	} {
		stdout.Reset()
		stderr.Reset()
		if code := runner.Run(context.Background(), args); code != ExitTargetConflict {
			t.Fatalf("%v code = %d, stdout = %q, stderr = %q", args, code, stdout.String(), stderr.String())
		}
		if !strings.Contains(stdout.String(), "already configured outside") {
			t.Fatalf("%v stdout = %q", args, stdout.String())
		}
	}
}

func TestRunnerChecksMCPExecutableAndEnvironmentAtPlanTime(t *testing.T) {
	root := t.TempDir()
	catalogPath := filepath.Join(root, "agx.yaml")
	missingExecutable := filepath.Join(root, "missing-server")
	writeMCPCatalog(t, catalogPath, missingExecutable, []string{"stdio"}, true)
	t.Setenv("GITHUB_TOKEN", "token")
	runner, stdout, stderr, _ := planRunner(t)
	runMCPCommand(t, runner, stdout, stderr, "lock", "--catalog", catalogPath)
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"plan", "--catalog", catalogPath}); code != ExitAgentUnavailable || !strings.Contains(stderr.String(), "executable") {
		t.Fatalf("missing executable plan code = %d, stderr = %q", code, stderr.String())
	}

	serverExecutable := filepath.Join(root, "available-server")
	writeTestExecutable(t, serverExecutable)
	writeMCPCatalog(t, catalogPath, serverExecutable, []string{"stdio"}, true)
	t.Setenv("GITHUB_TOKEN", "")
	runMCPCommand(t, runner, stdout, stderr, "lock", "--catalog", catalogPath)
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"plan", "--catalog", catalogPath}); code != ExitAgentUnavailable || !strings.Contains(stderr.String(), "GITHUB_TOKEN") {
		t.Fatalf("missing environment plan code = %d, stderr = %q", code, stderr.String())
	}
}

func TestRunnerRejectsDuplicateMCPNamesAcrossCatalogs(t *testing.T) {
	t.Setenv("AGX_CONFIG_HOME", t.TempDir())
	t.Setenv("AGX_STORE_HOME", t.TempDir())
	root := t.TempDir()
	serverExecutable := filepath.Join(root, "shared-server")
	writeTestExecutable(t, serverExecutable)
	personalRoot := t.TempDir()
	workRoot := t.TempDir()
	personalPath := filepath.Join(personalRoot, "agx.yaml")
	workPath := filepath.Join(workRoot, "agx.yaml")
	writeNamedMCPCatalog(t, personalPath, "personal", serverExecutable, []string{"stdio"}, true)
	writeNamedMCPCatalog(t, workPath, "work", serverExecutable, []string{"stdio"}, true)
	registerCatalogFixtures(t, map[string]string{"personal": personalPath, "work": workPath})
	t.Setenv("GITHUB_TOKEN", "token")
	runner, stdout, stderr, _ := planRunner(t)
	for _, path := range []string{personalPath, workPath} {
		runMCPCommand(t, runner, stdout, stderr, "lock", "--catalog", path)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"plan", "--catalogs", "personal,work"}); code != ExitInvalidConfig {
		t.Fatalf("composed plan code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "same codex server name") {
		t.Fatalf("composed plan stderr = %q", stderr.String())
	}
}

func writeMCPCatalog(t *testing.T, path, executable string, args []string, enabled bool) {
	writeNamedMCPCatalog(t, path, "personal", executable, args, enabled)
}

func writeNamedMCPCatalog(t *testing.T, path, name, executable string, args []string, enabled bool) {
	t.Helper()
	content := fmt.Sprintf(`apiVersion: agx.dev/v1alpha1
kind: Catalog
metadata:
  name: %s
skills: {}
`, name)
	if enabled {
		content += fmt.Sprintf(`mcpServers:
  github:
    transport: stdio
    command:
      executable: %q
`, executable)
		if len(args) != 0 {
			content += "      args:\n"
			for _, arg := range args {
				content += fmt.Sprintf("        - %q\n", arg)
			}
		}
		content += `    environment:
      GITHUB_TOKEN:
        from: env
        name: GITHUB_TOKEN
    targets:
      codex: {}
`
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeTestExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func runMCPCommand(t *testing.T, runner *Runner, stdout, stderr interface{ Reset() }, args ...string) {
	t.Helper()
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), args); code != ExitSuccess {
		t.Fatalf("%s code = %d", args[0], code)
	}
}

func assertMCPConfig(t *testing.T, path string, values ...string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range values {
		if !strings.Contains(string(content), value) {
			t.Fatalf("Codex config %q does not contain %q", content, value)
		}
	}
	if strings.Count(string(content), mcpconfig.BeginMarker) != 1 || strings.Count(string(content), mcpconfig.EndMarker) != 1 {
		t.Fatalf("Codex config has invalid MCP markers: %q", content)
	}
}
