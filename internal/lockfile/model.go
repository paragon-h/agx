package lockfile

import (
	"errors"
	"fmt"
	"regexp"
	"time"
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
	APIVersion    string                 `json:"apiVersion" yaml:"apiVersion"`
	Kind          string                 `json:"kind" yaml:"kind"`
	CatalogDigest string                 `json:"catalogDigest" yaml:"catalogDigest"`
	Skills        map[string]LockedSkill `json:"skills" yaml:"skills"`
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
	for name, skill := range l.Skills {
		if err := skill.Validate(); err != nil {
			return fmt.Errorf("skill %q: %w", name, err)
		}
	}
	return nil
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
		if s.Source.Path == "" {
			return errors.New("local source requires path")
		}
		if s.Source.ResolvedCommit != "" {
			return errors.New("local source cannot have resolvedCommit")
		}
	case "git":
		if s.Source.Repository == "" || s.Source.RequestedRevision == "" {
			return errors.New("git source requires repository and requestedRevision")
		}
		if !commitPattern.MatchString(s.Source.ResolvedCommit) {
			return errors.New("git resolvedCommit must be a full lowercase commit SHA")
		}
	default:
		return fmt.Errorf("unsupported source type %q", s.Source.Type)
	}
	return nil
}

func validDigest(value string) bool {
	return digestPattern.MatchString(value)
}
