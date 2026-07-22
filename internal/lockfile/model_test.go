package lockfile

import "testing"

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestLocalSourceRequiresContentDigest(t *testing.T) {
	lock := Lockfile{
		APIVersion:    APIVersion,
		Kind:          Kind,
		CatalogDigest: testDigest,
		Skills: map[string]LockedSkill{
			"code-review": {
				Source:        LockedSource{Type: "local", Path: "skills/code-review"},
				ContentDigest: testDigest,
				LockedAt:      "2026-07-22T10:00:00Z",
			},
		},
	}
	if err := lock.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestGitSourceRejectsShortCommit(t *testing.T) {
	skill := LockedSkill{
		Source: LockedSource{
			Type:              "git",
			Repository:        "https://example.com/skills.git",
			RequestedRevision: "main",
			ResolvedCommit:    "deadbeef",
		},
		ContentDigest: testDigest,
		LockedAt:      "2026-07-22T10:00:00Z",
	}
	if err := skill.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want full commit error")
	}
}
