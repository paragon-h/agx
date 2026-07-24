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

func TestLockfileValidateAllowsEmptySkillsMap(t *testing.T) {
	lock := Lockfile{
		APIVersion:    APIVersion,
		Kind:          Kind,
		CatalogDigest: testDigest,
		Skills:        map[string]LockedSkill{},
	}
	if err := lock.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want empty lockfile to be valid", err)
	}
}

func TestLockfileValidateRejectsMissingSkillsMap(t *testing.T) {
	lock := Lockfile{APIVersion: APIVersion, Kind: Kind, CatalogDigest: testDigest}
	if err := lock.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want missing skills error")
	}
}

func TestLocalSourceAcceptsAbsoluteAndHomePaths(t *testing.T) {
	for _, sourcePath := range []string{"/opt/agent-skills/review", "~/agent-skills/review"} {
		t.Run(sourcePath, func(t *testing.T) {
			skill := LockedSkill{
				Source:        LockedSource{Type: "local", Path: sourcePath},
				ContentDigest: testDigest,
				LockedAt:      "2026-07-22T10:00:00Z",
			}
			if err := skill.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
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

func TestLocalSourceRejectsGitFields(t *testing.T) {
	skill := LockedSkill{
		Source: LockedSource{
			Type:       "local",
			Path:       "skills/example",
			Repository: "https://example.com/skills.git",
		},
		ContentDigest: testDigest,
		LockedAt:      "2026-07-22T10:00:00Z",
	}
	if err := skill.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want local Git field error")
	}
}

func TestGitSourceRejectsEmbeddedCredentials(t *testing.T) {
	skill := LockedSkill{
		Source: LockedSource{
			Type:              "git",
			Repository:        "https://token@example.com/skills.git",
			RequestedRevision: "main",
			ResolvedCommit:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		ContentDigest: testDigest,
		LockedAt:      "2026-07-22T10:00:00Z",
	}
	if err := skill.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want embedded credential error")
	}
}
