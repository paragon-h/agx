package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alanhuangch/agx/internal/registry"
)

func TestRunnerCatalogRegistryLifecycle(t *testing.T) {
	t.Setenv("AGX_CONFIG_HOME", t.TempDir())
	personalPath := writeCatalogRegistryFixture(t, "personal", "personal-skill")
	workPath := writeCatalogRegistryFixture(t, "work", "work-skill")
	var stdout, stderr bytes.Buffer
	runner := New(&stdout, &stderr, "dev")

	if code := runner.Run(context.Background(), []string{"catalog", "add", "personal", "--path", filepath.Dir(personalPath)}); code != ExitSuccess {
		t.Fatalf("add personal code = %d, stderr = %q", code, stderr.String())
	}
	value, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	if value.Active != "personal" || value.Catalogs["personal"].Path != personalPath {
		t.Fatalf("registry after first add = %#v", value)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"catalog", "add", "work", "--path=" + workPath}); code != ExitSuccess {
		t.Fatalf("add work code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"catalog", "list", "--json"}); code != ExitSuccess {
		t.Fatalf("list code = %d, stderr = %q", code, stderr.String())
	}
	var entries []catalogListEntry
	if err := json.Unmarshal(stdout.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Name != "personal" || !entries[0].Active || entries[1].Name != "work" || entries[1].Active {
		t.Fatalf("catalog list = %#v", entries)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"catalog", "use", "work"}); code != ExitSuccess {
		t.Fatalf("use work code = %d, stderr = %q", code, stderr.String())
	}
	value, err = registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	if value.Active != "work" {
		t.Fatalf("active catalog = %q, want work", value.Active)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"catalog", "remove", "work"}); code != ExitSuccess {
		t.Fatalf("remove work code = %d, stderr = %q", code, stderr.String())
	}
	value, err = registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	if value.Active != "" || len(value.Catalogs) != 1 {
		t.Fatalf("registry after remove = %#v", value)
	}
	if _, err := os.Stat(workPath); err != nil {
		t.Fatalf("remove deleted Catalog file: %v", err)
	}
}

func TestRunnerCatalogAddRejectsDuplicatesAndNameMismatch(t *testing.T) {
	t.Setenv("AGX_CONFIG_HOME", t.TempDir())
	personalPath := writeCatalogRegistryFixture(t, "personal", "personal-skill")
	workPath := writeCatalogRegistryFixture(t, "work", "work-skill")
	var stdout, stderr bytes.Buffer
	runner := New(&stdout, &stderr, "dev")

	if code := runner.Run(context.Background(), []string{"catalog", "add", "personal", "--path", personalPath}); code != ExitSuccess {
		t.Fatalf("initial add code = %d, stderr = %q", code, stderr.String())
	}
	for _, test := range []struct {
		name string
		args []string
		code string
	}{
		{name: "duplicate name", args: []string{"catalog", "add", "personal", "--path", personalPath}, code: "AGX_CATALOG_EXISTS"},
		{name: "metadata mismatch", args: []string{"catalog", "add", "other", "--path", workPath}, code: "AGX_CATALOG_NAME_MISMATCH"},
	} {
		t.Run(test.name, func(t *testing.T) {
			stdout.Reset()
			stderr.Reset()
			if code := runner.Run(context.Background(), test.args); code == ExitSuccess {
				t.Fatalf("command succeeded, stderr = %q", stderr.String())
			}
			if !strings.Contains(stderr.String(), test.code) {
				t.Fatalf("stderr = %q, want %s", stderr.String(), test.code)
			}
		})
	}

	value, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	value.Catalogs["legacy"] = registry.Entry{Path: workPath}
	if err := registry.Save(value); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"catalog", "add", "work", "--path", workPath}); code != ExitFailure {
		t.Fatalf("duplicate path code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "AGX_CATALOG_EXISTS") {
		t.Fatalf("duplicate path stderr = %q", stderr.String())
	}
}

func TestResolveCatalogPathPrecedenceAndUnavailableActive(t *testing.T) {
	t.Setenv("AGX_CONFIG_HOME", t.TempDir())
	activePath := writeCatalogRegistryFixture(t, "active", "active-skill")
	localPath := writeCatalogRegistryFixture(t, "local", "local-skill")
	explicitPath := writeCatalogRegistryFixture(t, "explicit", "explicit-skill")
	value := registry.New()
	value.Catalogs["active"] = registry.Entry{Path: activePath}
	value.Active = "active"
	if err := registry.Save(value); err != nil {
		t.Fatal(err)
	}

	workingDirectory := t.TempDir()
	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workingDirectory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDirectory) })

	if got, err := resolveCatalogPath(""); err != nil || got != activePath {
		t.Fatalf("active resolution = %q, %v; want %q", got, err, activePath)
	}
	localData, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatal(err)
	}
	localInWorkingDirectory := filepath.Join(workingDirectory, "agx.yaml")
	if err := os.WriteFile(localInWorkingDirectory, localData, 0o644); err != nil {
		t.Fatal(err)
	}
	wantLocalPath, err := filepath.Abs("agx.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := resolveCatalogPath(""); err != nil || got != wantLocalPath {
		t.Fatalf("local resolution = %q, %v; want %q", got, err, wantLocalPath)
	}
	if got, err := resolveCatalogPath(explicitPath); err != nil || got != explicitPath {
		t.Fatalf("explicit resolution = %q, %v; want %q", got, err, explicitPath)
	}

	if err := os.Remove(localInWorkingDirectory); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(activePath); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveCatalogPath(""); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("unavailable active error = %v", err)
	}
}

func TestRunnerListComposesRegisteredCatalogs(t *testing.T) {
	t.Setenv("AGX_CONFIG_HOME", t.TempDir())
	personalPath := writeCatalogRegistryFixture(t, "personal", "review")
	workPath := writeCatalogRegistryFixture(t, "work", "deploy")
	value := registry.New()
	value.Active = "personal"
	value.Catalogs["personal"] = registry.Entry{Path: personalPath}
	value.Catalogs["work"] = registry.Entry{Path: workPath}
	if err := registry.Save(value); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	runner := New(&stdout, &stderr, "dev")
	if code := runner.Run(context.Background(), []string{"list", "--catalogs", "work,personal"}); code != ExitSuccess {
		t.Fatalf("list code = %d, stderr = %q", code, stderr.String())
	}
	want := "personal/review\tlocal\tcodex\nwork/deploy\tlocal\tcodex\n"
	if stdout.String() != want {
		t.Fatalf("list = %q, want %q", stdout.String(), want)
	}
}

func TestLoadCatalogCollectionRejectsInvalidSelection(t *testing.T) {
	if _, err := loadCatalogCollection("agx.yaml", "personal"); err == nil || !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("combined flags error = %v", err)
	}
	if _, err := parseCatalogNames("personal,personal"); err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Fatalf("duplicate names error = %v", err)
	}
}

func writeCatalogRegistryFixture(t *testing.T, name, skill string) string {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "agx.yaml")
	content := `apiVersion: agx.dev/v1alpha1
kind: Catalog
metadata:
  name: ` + name + `
skills:
  ` + skill + `:
    source:
      type: local
      path: skills/` + skill + `
    targets:
      codex: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
