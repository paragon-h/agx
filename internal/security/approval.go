package security

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alanhuangch/agx/internal/catalog"
	"github.com/alanhuangch/agx/internal/lockfile"
)

const (
	AdapterSecurityVersion = "copy-v1"
	// PolicyDigest must change whenever the built-in audit or approval policy changes.
	PolicyDigest = "sha256:6e6cbd5fbbb3dada37f52fe03188789025c0c568f37024af974b6915efca20d6"
)

// ApprovalKey deliberately lives outside the lockfile model. Any change to one
// of its fields invalidates a prior local approval.
type ApprovalKey struct {
	ResolvedCommit         string `json:"resolvedCommit,omitempty"`
	ContentDigest          string `json:"contentDigest"`
	OverlayDigest          string `json:"overlayDigest,omitempty"`
	AdapterSecurityVersion string `json:"adapterSecurityVersion"`
	PolicyDigest           string `json:"policyDigest"`
}

type Approval struct {
	SkillQualifiedName string      `json:"skill"`
	Key                ApprovalKey `json:"key"`
	ApprovedAt         string      `json:"approvedAt"`
}

func KeyFor(skill lockfile.LockedSkill) ApprovalKey {
	return ApprovalKey{
		ResolvedCommit:         skill.Source.ResolvedCommit,
		ContentDigest:          skill.ContentDigest,
		OverlayDigest:          skill.OverlayDigest,
		AdapterSecurityVersion: AdapterSecurityVersion,
		PolicyDigest:           PolicyDigest,
	}
}

func (a Approval) Validate() error {
	if !validQualifiedName(a.SkillQualifiedName) {
		return errors.New("approval skill name is invalid")
	}
	if !validDigest(a.Key.ContentDigest) || (a.Key.OverlayDigest != "" && !validDigest(a.Key.OverlayDigest)) {
		return errors.New("approval content digests are invalid")
	}
	if a.Key.ResolvedCommit != "" && !validCommit(a.Key.ResolvedCommit) {
		return errors.New("approval resolved commit is invalid")
	}
	if a.Key.AdapterSecurityVersion == "" || !validDigest(a.Key.PolicyDigest) {
		return errors.New("approval security policy key is invalid")
	}
	if _, err := time.Parse(time.RFC3339, a.ApprovedAt); err != nil {
		return fmt.Errorf("approval timestamp is invalid: %w", err)
	}
	return nil
}

func validQualifiedName(value string) bool {
	parts := strings.Split(value, "/")
	return len(parts) == 2 && catalog.ValidName(parts[0]) && catalog.ValidName(parts[1])
}

func validDigest(value string) bool {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, char := range value[7:] {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}

func validCommit(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}
