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
	got, err := document.Resolve("skills/code-review")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "skills", "code-review"); got != want {
		t.Fatalf("Resolve() = %q, want %q", got, want)
	}
}

func TestDocumentResolveAbsolutePath(t *testing.T) {
	document := Document{Root: t.TempDir()}
	absolute := filepath.Join(t.TempDir(), "skills", "code-review")
	got, err := document.Resolve(absolute)
	if err != nil {
		t.Fatal(err)
	}
	if got != absolute {
		t.Fatalf("Resolve() = %q, want %q", got, absolute)
	}
}

func TestDocumentResolveUserHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	document := Document{Root: t.TempDir()}
	got, err := document.Resolve("~/shared-skills/code-review")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "shared-skills", "code-review"); got != want {
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
