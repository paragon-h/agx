package catalog

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Document struct {
	Catalog Catalog
	Path    string
	Root    string
}

func Load(path string) (Document, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Document{}, err
	}
	file, err := os.Open(absolute)
	if err != nil {
		return Document{}, err
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	var value Catalog
	if err := decoder.Decode(&value); err != nil {
		return Document{}, fmt.Errorf("decode catalog: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Document{}, errors.New("catalog must contain exactly one YAML document")
		}
		return Document{}, fmt.Errorf("decode trailing catalog document: %w", err)
	}
	if err := value.Validate(); err != nil {
		return Document{}, fmt.Errorf("validate catalog: %w", err)
	}
	return Document{Catalog: value, Path: absolute, Root: filepath.Dir(absolute)}, nil
}

func (d Document) Resolve(relative string) string {
	return filepath.Join(d.Root, filepath.FromSlash(relative))
}
