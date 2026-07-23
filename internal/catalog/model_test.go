package catalog

import "testing"

func TestCatalogValidate(t *testing.T) {
	catalog := Catalog{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: "personal"},
		Skills: map[string]Skill{
			"code-review": {
				Source:  Source{Type: "local", Path: "skills/code-review"},
				Targets: map[string]TargetConfig{"codex": {}},
			},
		},
	}

	if err := catalog.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got, want := QualifiedName("personal", "code-review"), "personal/code-review"; got != want {
		t.Fatalf("QualifiedName() = %q, want %q", got, want)
	}
}

func TestCatalogAcceptsAbsoluteAndHomeLocalPaths(t *testing.T) {
	for _, sourcePath := range []string{"/opt/agent-skills/review", "~/agent-skills/review"} {
		t.Run(sourcePath, func(t *testing.T) {
			catalog := Catalog{
				APIVersion: APIVersion,
				Kind:       Kind,
				Metadata:   Metadata{Name: "personal"},
				Skills: map[string]Skill{
					"review": {
						Source:  Source{Type: "local", Path: sourcePath},
						Targets: map[string]TargetConfig{"codex": {}},
					},
				},
			}
			if err := catalog.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestCatalogRejectsEscapingRelativeLocalPath(t *testing.T) {
	catalog := Catalog{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: "personal"},
		Skills: map[string]Skill{
			"unsafe": {
				Source:  Source{Type: "local", Path: "../outside"},
				Targets: map[string]TargetConfig{"codex": {}},
			},
		},
	}

	if err := catalog.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want path escape error")
	}
}

func TestCatalogRejectsInvalidLocalPath(t *testing.T) {
	for _, sourcePath := range []string{`..\\outside`, `~other/skills`, "bad\npath"} {
		t.Run(sourcePath, func(t *testing.T) {
			catalog := Catalog{
				APIVersion: APIVersion,
				Kind:       Kind,
				Metadata:   Metadata{Name: "personal"},
				Skills: map[string]Skill{
					"unsafe": {
						Source:  Source{Type: "local", Path: sourcePath},
						Targets: map[string]TargetConfig{"codex": {}},
					},
				},
			}
			if err := catalog.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want portable path escape error")
			}
		})
	}
}

func TestCatalogRejectsEmbeddedGitCredentials(t *testing.T) {
	catalog := Catalog{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: "personal"},
		Skills: map[string]Skill{
			"unsafe": {
				Source: Source{
					Type:       "git",
					Repository: "https://token@example.com/skills.git",
					Revision:   "main",
				},
				Targets: map[string]TargetConfig{"codex": {}},
			},
		},
	}
	if err := catalog.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want embedded credential error")
	}
}

func TestValidateGitRepositoryRejectsSSHPassword(t *testing.T) {
	if err := ValidateGitRepository("ssh://git:secret@example.com/skills.git"); err == nil {
		t.Fatal("ValidateGitRepository() error = nil, want embedded SSH credential error")
	}
}

func TestValidateGitRepositoryRejectsRemoteHelper(t *testing.T) {
	if err := ValidateGitRepository("ext::sh -c dangerous"); err == nil {
		t.Fatal("ValidateGitRepository() error = nil, want remote helper rejection")
	}
}

func TestValidateGitRepositoryAcceptsStandardSources(t *testing.T) {
	for _, repository := range []string{
		"https://github.com/example/skills.git",
		"ssh://git@github.com/example/skills.git",
		"git@github.com:example/skills.git",
		"/tmp/skills",
		"../skills",
		"file:///tmp/skills",
	} {
		t.Run(repository, func(t *testing.T) {
			if err := ValidateGitRepository(repository); err != nil {
				t.Fatalf("ValidateGitRepository() error = %v", err)
			}
		})
	}
}
