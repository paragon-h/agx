package security

import (
	"testing"
	"time"

	"github.com/alanhuangch/agx/internal/lockfile"
)

func TestSaveLoadAndInvalidateApproval(t *testing.T) {
	t.Setenv("AGX_STATE_HOME", t.TempDir())
	locked := lockfile.LockedSkill{
		Source:        lockfile.LockedSource{Type: "git", ResolvedCommit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		ContentDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
	approval := Approval{
		SkillQualifiedName: "personal/review",
		Key:                KeyFor(locked),
		ApprovedAt:         time.Now().UTC().Format(time.RFC3339),
	}
	if err := SaveApproval(approval); err != nil {
		t.Fatal(err)
	}
	approved, err := IsApproved(approval.SkillQualifiedName, approval.Key)
	if err != nil || !approved {
		t.Fatalf("IsApproved() = %v, %v", approved, err)
	}
	changed := approval.Key
	changed.ContentDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	approved, err = IsApproved(approval.SkillQualifiedName, changed)
	if err != nil || approved {
		t.Fatalf("changed IsApproved() = %v, %v", approved, err)
	}
}
