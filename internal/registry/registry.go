package registry

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/paragon-h/agx/internal/catalog"
	"gopkg.in/yaml.v3"
)

const (
	APIVersion = "agx.dev/v1alpha1"
	Kind       = "CatalogRegistry"
)

type Config struct {
	APIVersion string           `json:"apiVersion" yaml:"apiVersion"`
	Kind       string           `json:"kind" yaml:"kind"`
	Active     string           `json:"active,omitempty" yaml:"active,omitempty"`
	Catalogs   map[string]Entry `json:"catalogs" yaml:"catalogs"`
}

type Entry struct {
	Path string `json:"path" yaml:"path"`
}

func New() Config {
	return Config{APIVersion: APIVersion, Kind: Kind, Catalogs: map[string]Entry{}}
}

func (c Config) Validate() error {
	if c.APIVersion != APIVersion || c.Kind != Kind {
		return errors.New("invalid Catalog Registry type metadata")
	}
	if c.Catalogs == nil {
		return errors.New("catalogs is required")
	}
	paths := make(map[string]string, len(c.Catalogs))
	for name, entry := range c.Catalogs {
		if !catalog.ValidName(name) {
			return fmt.Errorf("catalog %q has an invalid name", name)
		}
		if !filepath.IsAbs(entry.Path) {
			return fmt.Errorf("catalog %q path must be absolute", name)
		}
		cleaned := filepath.Clean(entry.Path)
		if existing, ok := paths[cleaned]; ok {
			return fmt.Errorf("catalogs %q and %q use the same path", existing, name)
		}
		paths[cleaned] = name
	}
	if c.Active != "" {
		if _, ok := c.Catalogs[c.Active]; !ok {
			return fmt.Errorf("active catalog %q is not registered", c.Active)
		}
	}
	return nil
}

func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return New(), nil
	}
	if err != nil {
		return Config{}, err
	}
	defer file.Close()
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	var value Config
	if err := decoder.Decode(&value); err != nil {
		return Config{}, fmt.Errorf("decode Catalog Registry: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Config{}, errors.New("Catalog Registry must contain exactly one YAML document")
		}
		return Config{}, fmt.Errorf("decode trailing Catalog Registry document: %w", err)
	}
	if err := value.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate Catalog Registry: %w", err)
	}
	return value, nil
}

func Save(value Config) error {
	if err := value.Validate(); err != nil {
		return err
	}
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".agx-catalogs-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	encoder := yaml.NewEncoder(file)
	encoder.SetIndent(2)
	if err := encoder.Encode(value); err != nil {
		encoder.Close()
		file.Close()
		return err
	}
	if err := encoder.Close(); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func Path() (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "catalogs.yaml"), nil
}

func Root() (string, error) {
	if override := os.Getenv("AGX_CONFIG_HOME"); override != "" {
		if !filepath.IsAbs(override) {
			return "", errors.New("AGX_CONFIG_HOME must be an absolute path")
		}
		return filepath.Clean(override), nil
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(root, "agx"), nil
}

func ResolveCatalogPath(value string) (string, error) {
	if value == "~" || strings.HasPrefix(value, "~/") || strings.HasPrefix(value, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve user home: %w", err)
		}
		if value == "~" {
			value = home
		} else {
			relative := strings.TrimLeft(value[1:], `/\`)
			value = filepath.Join(home, filepath.FromSlash(strings.ReplaceAll(relative, `\`, "/")))
		}
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		absolute = filepath.Join(absolute, "agx.yaml")
	}
	document, err := catalog.Load(absolute)
	if err != nil {
		return "", err
	}
	return document.Path, nil
}

func SortedNames(value Config) []string {
	names := make([]string, 0, len(value.Catalogs))
	for name := range value.Catalogs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
