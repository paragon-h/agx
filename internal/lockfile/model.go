package lockfile

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/paragon-h/agx/internal/catalog"
)

const (
	APIVersion = "agx.dev/v1alpha1"
	Kind       = "Lockfile"
)

var (
	commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type Lockfile struct {
	APIVersion    string                       `json:"apiVersion" yaml:"apiVersion"`
	Kind          string                       `json:"kind" yaml:"kind"`
	CatalogDigest string                       `json:"catalogDigest" yaml:"catalogDigest"`
	Skills        map[string]LockedSkill       `json:"skills" yaml:"skills"`
	Instructions  map[string]LockedInstruction `json:"instructions,omitempty" yaml:"instructions,omitempty"`
}

type LockedInstruction struct {
	Sources       []LockedInstructionSource `json:"sources" yaml:"sources"`
	Content       string                    `json:"content" yaml:"content"`
	ContentDigest string                    `json:"contentDigest" yaml:"contentDigest"`
	LockedAt      string                    `json:"lockedAt" yaml:"lockedAt"`
}

type LockedInstructionSource struct {
	Path          string `json:"path" yaml:"path"`
	ContentDigest string `json:"contentDigest" yaml:"contentDigest"`
}

type LockedSkill struct {
	Source        LockedSource `json:"source" yaml:"source"`
	ContentDigest string       `json:"contentDigest" yaml:"contentDigest"`
	OverlayDigest string       `json:"overlayDigest,omitempty" yaml:"overlayDigest,omitempty"`
	LockedAt      string       `json:"lockedAt" yaml:"lockedAt"`
}

type LockedSource struct {
	Type              string `json:"type" yaml:"type"`
	Path              string `json:"path,omitempty" yaml:"path,omitempty"`
	Repository        string `json:"repository,omitempty" yaml:"repository,omitempty"`
	RequestedRevision string `json:"requestedRevision,omitempty" yaml:"requestedRevision,omitempty"`
	ResolvedCommit    string `json:"resolvedCommit,omitempty" yaml:"resolvedCommit,omitempty"`
}

func (l Lockfile) Validate() error {
	if l.APIVersion != APIVersion || l.Kind != Kind {
		return errors.New("invalid lockfile type metadata")
	}
	if !validDigest(l.CatalogDigest) {
		return errors.New("catalogDigest must be a sha256 digest")
	}
	if l.Skills == nil {
		return errors.New("skills is required")
	}
	for name, skill := range l.Skills {
		if !catalog.ValidName(name) {
			return fmt.Errorf("skill %q has an invalid short name", name)
		}
		if err := skill.Validate(); err != nil {
			return fmt.Errorf("skill %q: %w", name, err)
		}
	}
	for name, instruction := range l.Instructions {
		if !catalog.ValidName(name) {
			return fmt.Errorf("instructions %q has an invalid short name", name)
		}
		if err := instruction.Validate(); err != nil {
			return fmt.Errorf("instructions %q: %w", name, err)
		}
	}
	return nil
}

func (i LockedInstruction) Validate() error {
	if len(i.Sources) == 0 {
		return errors.New("sources must contain at least one Markdown file")
	}
	seen := make(map[string]struct{}, len(i.Sources))
	for index, source := range i.Sources {
		if !catalog.ValidLocalPath(source.Path) || !validDigest(source.ContentDigest) {
			return fmt.Errorf("source %d is invalid", index)
		}
		if _, exists := seen[source.Path]; exists {
			return fmt.Errorf("source path %q is duplicated", source.Path)
		}
		seen[source.Path] = struct{}{}
	}
	if !validDigest(i.ContentDigest) || digestContent(i.Content) != i.ContentDigest {
		return errors.New("content does not match contentDigest")
	}
	if _, err := time.Parse(time.RFC3339, i.LockedAt); err != nil {
		return errors.New("lockedAt must be an RFC 3339 timestamp")
	}
	return nil
}

func digestContent(content string) string {
	digest := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func (s LockedSkill) Validate() error {
	if !validDigest(s.ContentDigest) {
		return errors.New("contentDigest must be a sha256 digest")
	}
	if s.OverlayDigest != "" && !validDigest(s.OverlayDigest) {
		return errors.New("overlayDigest must be a sha256 digest")
	}
	if _, err := time.Parse(time.RFC3339, s.LockedAt); err != nil {
		return errors.New("lockedAt must be an RFC 3339 timestamp")
	}
	switch s.Source.Type {
	case "local":
		if !catalog.ValidLocalPath(s.Source.Path) {
			return errors.New("local source path must be catalog-relative, absolute, or use ~/ for the user home")
		}
		if s.Source.Repository != "" || s.Source.RequestedRevision != "" || s.Source.ResolvedCommit != "" {
			return errors.New("local source cannot contain Git resolution fields")
		}
	case "git":
		if s.Source.Repository == "" || s.Source.RequestedRevision == "" {
			return errors.New("git source requires repository and requestedRevision")
		}
		if err := catalog.ValidateGitRepository(s.Source.Repository); err != nil {
			return err
		}
		if strings.HasPrefix(s.Source.RequestedRevision, "-") || strings.ContainsAny(s.Source.RequestedRevision, "\r\n\x00") {
			return errors.New("git requestedRevision contains unsupported characters")
		}
		if !commitPattern.MatchString(s.Source.ResolvedCommit) {
			return errors.New("git resolvedCommit must be a full lowercase commit SHA")
		}
		if s.Source.Path != "" && !catalog.ValidRelativePath(s.Source.Path) {
			return errors.New("git source path must stay within the repository root")
		}
	default:
		return fmt.Errorf("unsupported source type %q", s.Source.Type)
	}
	return nil
}

func validDigest(value string) bool {
	return digestPattern.MatchString(value)
}
