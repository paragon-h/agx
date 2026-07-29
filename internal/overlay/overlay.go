package overlay

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/alanhuangch/agx/internal/catalog"
	"gopkg.in/yaml.v3"
)

const (
	APIVersion = "agx.dev/v1alpha1"
	Kind       = "Overlay"
)

type Manifest struct {
	APIVersion     string                  `yaml:"apiVersion"`
	Kind           string                  `yaml:"kind"`
	Rename         string                  `yaml:"rename,omitempty"`
	Content        Content                 `yaml:"content,omitempty"`
	DisableScripts []string                `yaml:"disableScripts,omitempty"`
	Targets        map[string]TargetConfig `yaml:"targets,omitempty"`
}

type Content struct {
	Prepend string `yaml:"prepend,omitempty"`
	Append  string `yaml:"append,omitempty"`
}

type TargetConfig struct {
	Metadata map[string]string `yaml:"metadata,omitempty"`
}

func Apply(root, overlayRoot string) error {
	manifest, err := validate(overlayRoot)
	if err != nil {
		return err
	}
	if err := applyContent(root, overlayRoot, manifest.Content); err != nil {
		return err
	}
	for _, relative := range manifest.DisableScripts {
		if !catalog.ValidRelativePath(relative) {
			return fmt.Errorf("disableScripts path %q must stay within the Skill root", relative)
		}
		path := filepath.Join(root, filepath.FromSlash(relative))
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("disable script %q: %w", relative, err)
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("disable script %q: target must be a regular file", relative)
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("disable script %q: %w", relative, err)
		}
	}
	return nil
}

// Validate parses an overlay and verifies every path that can be resolved
// without a Skill tree. Applying an overlay to a Skill still validates the
// target-specific disableScripts entries in Apply.
func Validate(overlayRoot string) error {
	_, err := validate(overlayRoot)
	return err
}

func validate(overlayRoot string) (Manifest, error) {
	manifest, err := load(filepath.Join(overlayRoot, "overlay.yaml"))
	if err != nil {
		return Manifest{}, err
	}
	if manifest.Rename != "" {
		return Manifest{}, errors.New("overlay rename is not supported yet")
	}
	if len(manifest.Targets) > 0 {
		return Manifest{}, errors.New("target-specific overlay metadata is not supported yet")
	}
	if manifest.Content.Prepend != "" {
		if _, err := readOverlayFile(overlayRoot, manifest.Content.Prepend); err != nil {
			return Manifest{}, fmt.Errorf("read content.prepend: %w", err)
		}
	}
	if manifest.Content.Append != "" {
		if _, err := readOverlayFile(overlayRoot, manifest.Content.Append); err != nil {
			return Manifest{}, fmt.Errorf("read content.append: %w", err)
		}
	}
	for _, relative := range manifest.DisableScripts {
		if !catalog.ValidRelativePath(relative) {
			return Manifest{}, fmt.Errorf("disableScripts path %q must stay within the Skill root", relative)
		}
	}
	return manifest, nil
}

func load(path string) (Manifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("load overlay manifest: %w", err)
	}
	defer file.Close()
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode overlay manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Manifest{}, errors.New("overlay manifest must contain exactly one YAML document")
		}
		return Manifest{}, fmt.Errorf("decode trailing overlay manifest document: %w", err)
	}
	if manifest.APIVersion != APIVersion {
		return Manifest{}, fmt.Errorf("overlay apiVersion must be %q", APIVersion)
	}
	if manifest.Kind != Kind {
		return Manifest{}, fmt.Errorf("overlay kind must be %q", Kind)
	}
	if manifest.Content.Prepend != "" && !catalog.ValidRelativePath(manifest.Content.Prepend) {
		return Manifest{}, errors.New("content.prepend must stay within the overlay root")
	}
	if manifest.Content.Append != "" && !catalog.ValidRelativePath(manifest.Content.Append) {
		return Manifest{}, errors.New("content.append must stay within the overlay root")
	}
	return manifest, nil
}

func applyContent(root, overlayRoot string, content Content) error {
	if content.Prepend == "" && content.Append == "" {
		return nil
	}
	manifestPath := filepath.Join(root, "SKILL.md")
	info, err := os.Lstat(manifestPath)
	if err != nil {
		return fmt.Errorf("read Skill manifest: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Skill manifest must be a regular file")
	}
	original, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	result := original
	if content.Prepend != "" {
		prepend, err := readOverlayFile(overlayRoot, content.Prepend)
		if err != nil {
			return fmt.Errorf("read content.prepend: %w", err)
		}
		result = join(prepend, result)
	}
	if content.Append != "" {
		appendContent, err := readOverlayFile(overlayRoot, content.Append)
		if err != nil {
			return fmt.Errorf("read content.append: %w", err)
		}
		result = join(result, appendContent)
	}
	return os.WriteFile(manifestPath, result, info.Mode().Perm())
}

func readOverlayFile(root, relative string) ([]byte, error) {
	path := filepath.Join(root, filepath.FromSlash(relative))
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("overlay content must be a regular file")
	}
	return os.ReadFile(path)
}

func join(left, right []byte) []byte {
	result := append([]byte(nil), left...)
	if len(left) > 0 && len(right) > 0 && !bytes.HasSuffix(left, []byte("\n")) {
		result = append(result, '\n')
	}
	return append(result, right...)
}
