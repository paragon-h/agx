package catalog

import (
	"errors"
	"fmt"
	pathpkg "path"
	"regexp"
	"strings"
)

const (
	APIVersion = "agx.dev/v1alpha1"
	Kind       = "Catalog"
)

var (
	namePattern          = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?$`)
	windowsVolumePattern = regexp.MustCompile(`^[A-Za-z]:`)
)

type Catalog struct {
	APIVersion string           `json:"apiVersion" yaml:"apiVersion"`
	Kind       string           `json:"kind" yaml:"kind"`
	Metadata   Metadata         `json:"metadata" yaml:"metadata"`
	Defaults   Defaults         `json:"defaults,omitempty" yaml:"defaults,omitempty"`
	Skills     map[string]Skill `json:"skills" yaml:"skills"`
}

type Metadata struct {
	Name string `json:"name" yaml:"name"`
}

type Defaults struct {
	InstallStrategy string `json:"installStrategy,omitempty" yaml:"installStrategy,omitempty"`
	ConflictPolicy  string `json:"conflictPolicy,omitempty" yaml:"conflictPolicy,omitempty"`
}

type Skill struct {
	Source  Source                  `json:"source" yaml:"source"`
	Overlay string                  `json:"overlay,omitempty" yaml:"overlay,omitempty"`
	Targets map[string]TargetConfig `json:"targets" yaml:"targets"`
}

type Source struct {
	Type       string `json:"type" yaml:"type"`
	Path       string `json:"path,omitempty" yaml:"path,omitempty"`
	Repository string `json:"repository,omitempty" yaml:"repository,omitempty"`
	Revision   string `json:"revision,omitempty" yaml:"revision,omitempty"`
}

type TargetConfig struct {
	Enabled *bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
}

func (c Catalog) Validate() error {
	if c.APIVersion != APIVersion {
		return fmt.Errorf("apiVersion must be %q", APIVersion)
	}
	if c.Kind != Kind {
		return fmt.Errorf("kind must be %q", Kind)
	}
	if !ValidName(c.Metadata.Name) {
		return errors.New("metadata.name must be a lowercase resource name")
	}
	if len(c.Skills) == 0 {
		return errors.New("skills must contain at least one entry")
	}
	if c.Defaults.InstallStrategy != "" && c.Defaults.InstallStrategy != "auto" && c.Defaults.InstallStrategy != "copy" {
		return errors.New("defaults.installStrategy must be auto or copy in Milestone 1")
	}
	if c.Defaults.ConflictPolicy != "" && c.Defaults.ConflictPolicy != "error" {
		return errors.New("defaults.conflictPolicy must be error in Milestone 1")
	}
	for name, skill := range c.Skills {
		if !ValidName(name) {
			return fmt.Errorf("skill %q has an invalid short name", name)
		}
		if strings.Contains(name, "/") {
			return fmt.Errorf("skill %q must use a short name inside a catalog", name)
		}
		if err := skill.Validate(); err != nil {
			return fmt.Errorf("skill %q: %w", name, err)
		}
	}
	return nil
}

func (s Skill) Validate() error {
	if s.Overlay != "" && !ValidRelativePath(s.Overlay) {
		return errors.New("overlay path must stay within the catalog root")
	}
	switch s.Source.Type {
	case "local":
		if s.Source.Path == "" {
			return errors.New("local source requires path")
		}
		if !ValidRelativePath(s.Source.Path) {
			return errors.New("local source path must stay within the catalog root")
		}
		if s.Source.Repository != "" || s.Source.Revision != "" {
			return errors.New("local source cannot set repository or revision")
		}
	case "git":
		if s.Source.Repository == "" || s.Source.Revision == "" {
			return errors.New("git source requires repository and revision")
		}
		if s.Source.Path != "" && !ValidRelativePath(s.Source.Path) {
			return errors.New("git source path must stay within the repository root")
		}
	default:
		return fmt.Errorf("unsupported source type %q", s.Source.Type)
	}
	if len(s.Targets) == 0 {
		return errors.New("targets must contain at least one agent")
	}
	hasEnabledTarget := false
	for target := range s.Targets {
		if target != "codex" && target != "claude" {
			return fmt.Errorf("Milestone 1 does not support target %q", target)
		}
		config := s.Targets[target]
		if config.Enabled == nil || *config.Enabled {
			hasEnabledTarget = true
		}
	}
	if !hasEnabledTarget {
		return errors.New("targets must enable at least one agent")
	}
	return nil
}

func ValidName(name string) bool {
	return namePattern.MatchString(name)
}

func QualifiedName(catalogName, resourceName string) string {
	return catalogName + "/" + resourceName
}

func ValidRelativePath(value string) bool {
	normalized := strings.ReplaceAll(value, "\\", "/")
	cleaned := pathpkg.Clean(normalized)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return false
	}
	return !windowsVolumePattern.MatchString(cleaned)
}
