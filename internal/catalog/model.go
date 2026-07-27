package catalog

import (
	"errors"
	"fmt"
	"net/url"
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
	scpRepositoryPattern = regexp.MustCompile(`^(?:[^/@:]+@)?[^/:]+:[^:].+$`)
)

type Catalog struct {
	APIVersion   string                 `json:"apiVersion" yaml:"apiVersion"`
	Kind         string                 `json:"kind" yaml:"kind"`
	Metadata     Metadata               `json:"metadata" yaml:"metadata"`
	Defaults     Defaults               `json:"defaults,omitempty" yaml:"defaults,omitempty"`
	Skills       map[string]Skill       `json:"skills" yaml:"skills"`
	Instructions map[string]Instruction `json:"instructions,omitempty" yaml:"instructions,omitempty"`
	Profiles     map[string]Profile     `json:"profiles,omitempty" yaml:"profiles,omitempty"`
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

type Instruction struct {
	Sources []string                `json:"sources" yaml:"sources"`
	Targets map[string]TargetConfig `json:"targets" yaml:"targets"`
}

type Profile struct {
	Skills  ProfileSkills `json:"skills,omitempty" yaml:"skills,omitempty"`
	Targets []string      `json:"targets,omitempty" yaml:"targets,omitempty"`
}

type ProfileSkills struct {
	Include []string `json:"include,omitempty" yaml:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty" yaml:"exclude,omitempty"`
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
	if c.Skills == nil {
		return errors.New("skills is required")
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
	for name, instruction := range c.Instructions {
		if !ValidName(name) || strings.Contains(name, "/") {
			return fmt.Errorf("instructions %q has an invalid short name", name)
		}
		if err := instruction.Validate(); err != nil {
			return fmt.Errorf("instructions %q: %w", name, err)
		}
	}
	for name, profile := range c.Profiles {
		if !ValidName(name) || strings.Contains(name, "/") {
			return fmt.Errorf("profile %q has an invalid short name", name)
		}
		if err := profile.Validate(c.Metadata.Name, c.Skills); err != nil {
			return fmt.Errorf("profile %q: %w", name, err)
		}
	}
	return nil
}

func (i Instruction) Validate() error {
	if len(i.Sources) == 0 {
		return errors.New("sources must contain at least one Markdown file")
	}
	seen := make(map[string]struct{}, len(i.Sources))
	for _, source := range i.Sources {
		if !ValidLocalPath(source) {
			return fmt.Errorf("source path %q must be catalog-relative, absolute, or use ~/ for the user home", source)
		}
		if _, exists := seen[source]; exists {
			return fmt.Errorf("source path %q is duplicated", source)
		}
		seen[source] = struct{}{}
	}
	if len(i.Targets) == 0 {
		return errors.New("targets must contain at least one agent")
	}
	hasEnabledTarget := false
	for target, config := range i.Targets {
		if target != "codex" && target != "pi" && target != "opencode" {
			return fmt.Errorf("global Instructions do not support target %q", target)
		}
		if config.Enabled == nil || *config.Enabled {
			hasEnabledTarget = true
		}
	}
	if !hasEnabledTarget {
		return errors.New("targets must enable at least one agent")
	}
	return nil
}

func (p Profile) Validate(catalogName string, skills map[string]Skill) error {
	include, err := validateProfileSkillNames("include", catalogName, p.Skills.Include, skills)
	if err != nil {
		return err
	}
	exclude, err := validateProfileSkillNames("exclude", catalogName, p.Skills.Exclude, skills)
	if err != nil {
		return err
	}
	for name := range include {
		if _, exists := exclude[name]; exists {
			return fmt.Errorf("skill %q cannot appear in both include and exclude", name)
		}
	}
	if p.Targets != nil && len(p.Targets) == 0 {
		return errors.New("targets must contain at least one agent when specified")
	}
	seenTargets := make(map[string]struct{}, len(p.Targets))
	for _, target := range p.Targets {
		if !ValidTarget(target) {
			return fmt.Errorf("unsupported target %q", target)
		}
		if _, exists := seenTargets[target]; exists {
			return fmt.Errorf("target %q is duplicated", target)
		}
		seenTargets[target] = struct{}{}
	}
	return nil
}

func validateProfileSkillNames(field, catalogName string, names []string, skills map[string]Skill) (map[string]struct{}, error) {
	seen := make(map[string]struct{}, len(names))
	for _, reference := range names {
		referenceCatalog, name, qualified, err := ParseQualifiedName(reference)
		if err != nil {
			return nil, fmt.Errorf("%s skill %q is invalid", field, reference)
		}
		if !qualified {
			referenceCatalog = catalogName
		}
		if referenceCatalog == catalogName {
			if _, exists := skills[name]; !exists {
				return nil, fmt.Errorf("%s skill %q is not declared in the catalog", field, reference)
			}
		}
		normalized := QualifiedName(referenceCatalog, name)
		if _, exists := seen[normalized]; exists {
			return nil, fmt.Errorf("%s skill %q is duplicated", field, reference)
		}
		seen[normalized] = struct{}{}
	}
	return seen, nil
}

func ParseQualifiedName(value string) (catalogName, resourceName string, qualified bool, err error) {
	parts := strings.Split(value, "/")
	switch len(parts) {
	case 1:
		if !ValidName(parts[0]) {
			return "", "", false, errors.New("invalid resource name")
		}
		return "", parts[0], false, nil
	case 2:
		if !ValidName(parts[0]) || !ValidName(parts[1]) {
			return "", "", false, errors.New("invalid qualified resource name")
		}
		return parts[0], parts[1], true, nil
	default:
		return "", "", false, errors.New("qualified resource name must contain exactly one slash")
	}
}

func (s Skill) Validate() error {
	if s.Overlay != "" && !ValidLocalPath(s.Overlay) {
		return errors.New("overlay path must be catalog-relative, absolute, or use ~/ for the user home")
	}
	switch s.Source.Type {
	case "local":
		if s.Source.Path == "" {
			return errors.New("local source requires path")
		}
		if !ValidLocalPath(s.Source.Path) {
			return errors.New("local source path must be catalog-relative, absolute, or use ~/ for the user home")
		}
		if s.Source.Repository != "" || s.Source.Revision != "" {
			return errors.New("local source cannot set repository or revision")
		}
	case "git":
		if s.Source.Repository == "" || s.Source.Revision == "" {
			return errors.New("git source requires repository and revision")
		}
		if strings.HasPrefix(s.Source.Revision, "-") || strings.ContainsAny(s.Source.Revision, "\r\n\x00") {
			return errors.New("git revision contains unsupported characters")
		}
		if err := ValidateGitRepository(s.Source.Repository); err != nil {
			return err
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
		if !ValidTarget(target) {
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

func ValidTarget(target string) bool {
	return target == "codex" || target == "claude" || target == "pi" || target == "opencode"
}

func ValidateGitRepository(repository string) error {
	if strings.HasPrefix(repository, "-") || strings.ContainsAny(repository, "\r\n\x00") {
		return errors.New("git repository contains unsupported characters")
	}
	if strings.Contains(repository, "::") {
		return errors.New("Git remote helpers are not supported as repository sources")
	}
	if !strings.Contains(repository, "://") && scpRepositoryPattern.MatchString(repository) {
		return nil
	}
	parsed, err := url.Parse(repository)
	if err != nil {
		return fmt.Errorf("git repository is invalid: %w", err)
	}
	if parsed.Scheme != "" && parsed.Scheme != "http" && parsed.Scheme != "https" && parsed.Scheme != "ssh" && parsed.Scheme != "git" && parsed.Scheme != "file" && !windowsVolumePattern.MatchString(repository) {
		return fmt.Errorf("git repository scheme %q is not supported", parsed.Scheme)
	}
	if parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword {
			return errors.New("git repository must not contain embedded credentials")
		}
	}
	if (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.User != nil {
		return errors.New("git repository must not contain embedded HTTP credentials")
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

func ValidLocalPath(value string) bool {
	if value == "" || strings.ContainsAny(value, "\r\n\x00") {
		return false
	}
	if value == "~" || strings.HasPrefix(value, "~/") || strings.HasPrefix(value, `~\`) {
		return true
	}
	if strings.HasPrefix(value, "~") {
		return false
	}
	return portableAbsolutePath(value) || ValidRelativePath(value)
}

func portableAbsolutePath(value string) bool {
	normalized := strings.ReplaceAll(value, `\`, "/")
	if strings.HasPrefix(normalized, "/") {
		return true
	}
	return len(normalized) >= 3 && windowsVolumePattern.MatchString(normalized) && normalized[2] == '/'
}
