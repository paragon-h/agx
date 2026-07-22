package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadStrictYAML(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "agx.yaml")
	data := `apiVersion: agx.dev/v1alpha1
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
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	document, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := document.Catalog.Metadata.Name, "personal"; got != want {
		t.Fatalf("metadata.name = %q, want %q", got, want)
	}
	if got, want := document.Resolve("skills/code-review"), filepath.Join(root, "skills", "code-review"); got != want {
		t.Fatalf("Resolve() = %q, want %q", got, want)
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agx.yaml")
	data := strings.Replace(`apiVersion: agx.dev/v1alpha1
kind: Catalog
metadata:
  name: personal
skills:
  example:
    source:
      type: local
      path: skills/example
    targets:
      codex: {}
`, "metadata:\n", "metadata:\n  unexpected: true\n", 1)
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want unknown field error")
	}
}
