package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paragon-h/agx/internal/catalog"
	"github.com/paragon-h/agx/internal/lockfile"
)

func TestRunnerInitCreatesEmptyCatalog(t *testing.T) {
	t.Setenv("AGX_STORE_HOME", t.TempDir())
	root := filepath.Join(t.TempDir(), "personal-catalog")
	catalogPath := filepath.Join(root, "agx.yaml")
	var stdout, stderr bytes.Buffer
	runner := New(&stdout, &stderr, "dev")
	if code := runner.Run(context.Background(), []string{"init", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("init code = %d, stderr = %q", code, stderr.String())
	}
	document, err := catalog.Load(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	if document.Catalog.Metadata.Name != "personal-catalog" || len(document.Catalog.Skills) != 0 {
		t.Fatalf("initialized catalog = %#v", document.Catalog)
	}
	for _, directory := range []string{"skills", "overlays", "instructions"} {
		info, err := os.Stat(filepath.Join(root, directory))
		if err != nil || !info.IsDir() {
			t.Fatalf("initialized directory %q: info=%v err=%v", directory, info, err)
		}
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("empty catalog lock code = %d, stderr = %q", code, stderr.String())
	}
	locked, err := lockfile.Load(filepath.Join(root, "agx.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if len(locked.Skills) != 0 {
		t.Fatalf("empty lockfile skills = %#v", locked.Skills)
	}
}

func TestRunnerInitUsesExplicitNameAndRefusesOverwrite(t *testing.T) {
	root := t.TempDir()
	catalogPath := filepath.Join(root, "nested", "agx.yaml")
	var stdout, stderr bytes.Buffer
	runner := New(&stdout, &stderr, "dev")
	if code := runner.Run(context.Background(), []string{"init", "--catalog", catalogPath, "--name", "personal"}); code != ExitSuccess {
		t.Fatalf("init code = %d, stderr = %q", code, stderr.String())
	}
	before, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"init", "--catalog", catalogPath, "--name", "other"}); code != ExitFailure {
		t.Fatalf("repeat init code = %d, want %d", code, ExitFailure)
	}
	after, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || !strings.Contains(stderr.String(), "AGX_INIT_EXISTS") {
		t.Fatalf("repeat init changed catalog or returned wrong error: %q", stderr.String())
	}
}

func TestRunnerInitRejectsInvalidName(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	runner := New(&stdout, &stderr, "dev")
	if code := runner.Run(context.Background(), []string{"init", "--catalog", filepath.Join(root, "agx.yaml"), "--name", "Invalid Name"}); code != ExitInvalidConfig {
		t.Fatalf("invalid name code = %d, want %d", code, ExitInvalidConfig)
	}
	if _, err := os.Stat(filepath.Join(root, "agx.yaml")); !os.IsNotExist(err) {
		t.Fatalf("invalid init created catalog: %v", err)
	}
}
