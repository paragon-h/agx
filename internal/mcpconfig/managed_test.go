package mcpconfig

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderUpdateAndRemovePreservesUserTOML(t *testing.T) {
	existing := []byte("model = \"gpt-5\"\n\n[mcp_servers.personal]\ncommand = \"personal-server\"\n")
	managed, err := Compose(map[string]Server{
		"github": {Executable: "github-mcp-server", Args: []string{"stdio"}, EnvVars: []string{"GITHUB_TOKEN"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := Render(existing, managed, []string{"github"})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"model = \"gpt-5\"", "[mcp_servers.personal]", BeginMarker, "[mcp_servers.\"github\"]", `env_vars = ["GITHUB_TOKEN"]`} {
		if !bytes.Contains(rendered, []byte(value)) {
			t.Fatalf("rendered config does not contain %q: %s", value, rendered)
		}
	}
	if strings.Contains(string(rendered), "secret-value") {
		t.Fatalf("rendered config contains a secret value: %s", rendered)
	}
	updated, err := Compose(map[string]Server{"github": {Executable: "github-mcp-server", Args: []string{"stdio", "--read-only"}}})
	if err != nil {
		t.Fatal(err)
	}
	rendered, err = Render(rendered, updated, []string{"github"})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(rendered, []byte(BeginMarker)) != 1 || !bytes.Contains(rendered, []byte("--read-only")) {
		t.Fatalf("updated config = %s", rendered)
	}
	remaining, found, err := Remove(rendered)
	if err != nil || !found {
		t.Fatalf("Remove() found = %v, err = %v", found, err)
	}
	if !bytes.Equal(remaining, existing) {
		t.Fatalf("remaining config = %q, want %q", remaining, existing)
	}
}

func TestRenderRejectsUnmanagedNameCollisionAndInvalidTOML(t *testing.T) {
	managed, err := Compose(map[string]Server{"github": {Executable: "github-mcp-server"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, existing := range [][]byte{
		[]byte("[mcp_servers.github]\ncommand = \"other\"\n"),
		[]byte("broken = [\n"),
	} {
		if _, err := Render(existing, managed, []string{"github"}); err == nil {
			t.Fatalf("Render(%q) error = nil", existing)
		}
	}
}

func TestParseRejectsMalformedMarkers(t *testing.T) {
	for _, content := range [][]byte{
		[]byte(BeginMarker + "\nmissing end\n"),
		[]byte("prefix " + BeginMarker + "\n" + EndMarker + "\n"),
		[]byte(BeginMarker + "\na = 1\n" + EndMarker + "\n" + BeginMarker + "\nb = 2\n" + EndMarker + "\n"),
	} {
		if _, err := Parse(content); err == nil {
			t.Fatalf("Parse(%q) error = nil", content)
		}
	}
}
