package app

import (
	"fmt"
	"strings"

	"github.com/paragon-h/agx/internal/catalog"
	"github.com/paragon-h/agx/internal/registry"
)

func loadCatalogCollection(explicitPath, registeredNames string) (catalog.Collection, error) {
	if explicitPath != "" && registeredNames != "" {
		return catalog.Collection{}, fmt.Errorf("--catalog and --catalogs cannot be used together")
	}
	if registeredNames == "" {
		path, err := resolveCatalogPath(explicitPath)
		if err != nil {
			return catalog.Collection{}, err
		}
		document, err := catalog.Load(path)
		if err != nil {
			return catalog.Collection{}, err
		}
		return catalog.NewCollection([]catalog.Document{document})
	}

	names, err := parseCatalogNames(registeredNames)
	if err != nil {
		return catalog.Collection{}, err
	}
	value, err := registry.Load()
	if err != nil {
		return catalog.Collection{}, err
	}
	documents := make([]catalog.Document, 0, len(names))
	for _, name := range names {
		entry, exists := value.Catalogs[name]
		if !exists {
			return catalog.Collection{}, fmt.Errorf("catalog %q is not registered", name)
		}
		document, loadErr := catalog.Load(entry.Path)
		if loadErr != nil {
			return catalog.Collection{}, fmt.Errorf("catalog %q is unavailable: %w", name, loadErr)
		}
		if document.Catalog.Metadata.Name != name {
			return catalog.Collection{}, fmt.Errorf("registered catalog %q has metadata.name %q", name, document.Catalog.Metadata.Name)
		}
		documents = append(documents, document)
	}
	return catalog.NewCollection(documents)
}

func parseCatalogNames(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	names := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if !catalog.ValidName(name) {
			return nil, fmt.Errorf("catalog name %q is invalid", name)
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("catalog %q is listed more than once", name)
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names, nil
}
