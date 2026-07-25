package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/paragon-h/agx/internal/catalog"
	"github.com/paragon-h/agx/internal/registry"
)

func resolveCatalogPath(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	local, err := filepath.Abs("agx.yaml")
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(local); err == nil {
		return local, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	registered, err := registry.Load()
	if err != nil {
		return "", err
	}
	if registered.Active == "" {
		return "", errors.New("no Catalog was specified, no agx.yaml exists in the current directory, and no active Catalog is registered")
	}
	entry, ok := registered.Catalogs[registered.Active]
	if !ok {
		return "", fmt.Errorf("active Catalog %q is not registered", registered.Active)
	}
	document, err := catalog.Load(entry.Path)
	if err != nil {
		return "", fmt.Errorf("active Catalog %q is unavailable: %w", registered.Active, err)
	}
	if document.Catalog.Metadata.Name != registered.Active {
		return "", fmt.Errorf("active Catalog %q has metadata.name %q", registered.Active, document.Catalog.Metadata.Name)
	}
	return document.Path, nil
}
