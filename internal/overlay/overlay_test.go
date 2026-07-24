package overlay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyContentAndDisableScript(t *testing.T) {
	root := t.TempDir()
	overlayRoot := t.TempDir()
	writeTestFile(t, filepath.Join(root, "SKILL.md"), "# Original\n", 0o644)
	writeTestFile(t, filepath.Join(root, "scripts", "upload.sh"), "#!/bin/sh\n", 0o755)
	writeTestFile(t, filepath.Join(overlayRoot, "overlay.yaml"), `apiVersion: agx.dev/v1alpha1
kind: Overlay
content:
  prepend: prepend.md
  append: append.md
disableScripts:
  - scripts/upload.sh
`, 0o644)
	writeTestFile(t, filepath.Join(overlayRoot, "prepend.md"), "Before", 0o644)
	writeTestFile(t, filepath.Join(overlayRoot, "append.md"), "After\n", 0o644)

	if err := Apply(root, overlayRoot); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "Before\n# Original\nAfter\n"; got != want {
		t.Fatalf("SKILL.md = %q, want %q", got, want)
	}
	if _, err := os.Lstat(filepath.Join(root, "scripts", "upload.sh")); !os.IsNotExist(err) {
		t.Fatalf("disabled script still exists: %v", err)
	}
}

func TestApplyRejectsEscapingPath(t *testing.T) {
	root := t.TempDir()
	overlayRoot := t.TempDir()
	writeTestFile(t, filepath.Join(root, "SKILL.md"), "# Original\n", 0o644)
	writeTestFile(t, filepath.Join(overlayRoot, "overlay.yaml"), `apiVersion: agx.dev/v1alpha1
kind: Overlay
content:
  prepend: ../outside.md
`, 0o644)

	err := Apply(root, overlayRoot)
	if err == nil || !strings.Contains(err.Error(), "stay within") {
		t.Fatalf("Apply() error = %v, want path validation error", err)
	}
}

func TestApplyRejectsUnsupportedTargetMetadata(t *testing.T) {
	root := t.TempDir()
	overlayRoot := t.TempDir()
	writeTestFile(t, filepath.Join(root, "SKILL.md"), "# Original\n", 0o644)
	writeTestFile(t, filepath.Join(overlayRoot, "overlay.yaml"), `apiVersion: agx.dev/v1alpha1
kind: Overlay
targets:
  opencode:
    metadata:
      category: frontend
`, 0o644)

	err := Apply(root, overlayRoot)
	if err == nil || !strings.Contains(err.Error(), "target-specific") {
		t.Fatalf("Apply() error = %v, want unsupported target metadata error", err)
	}
}

func writeTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}
