package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadAndResolveCatalogPath(t *testing.T) {
	configRoot := t.TempDir()
	t.Setenv("AGX_CONFIG_HOME", configRoot)
	catalogRoot := t.TempDir()
	catalogPath := filepath.Join(catalogRoot, "agx.yaml")
	if err := os.WriteFile(catalogPath, []byte(`apiVersion: agx.dev/v1alpha1
kind: Catalog
metadata:
  name: personal
skills: {}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolveCatalogPath(catalogRoot)
	if err != nil {
		t.Fatal(err)
	}
	value := New()
	value.Catalogs["personal"] = Entry{Path: resolved}
	value.Active = "personal"
	if err := Save(value); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Active != "personal" || loaded.Catalogs["personal"].Path != catalogPath {
		t.Fatalf("loaded registry = %#v", loaded)
	}
	info, err := os.Stat(filepath.Join(configRoot, "catalogs.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("registry permissions = %o, want private file", info.Mode().Perm())
	}
}

func TestConfigRejectsMissingActiveCatalog(t *testing.T) {
	value := New()
	value.Active = "missing"
	if err := value.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want missing active catalog error")
	}
}

func TestConfigRejectsDuplicateCatalogPaths(t *testing.T) {
	value := New()
	path := filepath.Join(t.TempDir(), "agx.yaml")
	value.Catalogs["one"] = Entry{Path: path}
	value.Catalogs["two"] = Entry{Path: path}
	if err := value.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want duplicate path error")
	}
}

func TestRootRejectsRelativeOverride(t *testing.T) {
	t.Setenv("AGX_CONFIG_HOME", "relative")
	if _, err := Root(); err == nil {
		t.Fatal("Root() error = nil, want absolute path error")
	}
}
