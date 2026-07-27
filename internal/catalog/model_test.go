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

func TestCatalogValidateAllowsEmptySkillsMap(t *testing.T) {
	catalog := Catalog{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: "personal"},
		Skills:     map[string]Skill{},
	}
	if err := catalog.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want empty catalog to be valid", err)
	}
}

func TestCatalogValidateRejectsMissingSkillsMap(t *testing.T) {
	catalog := Catalog{APIVersion: APIVersion, Kind: Kind, Metadata: Metadata{Name: "personal"}}
	if err := catalog.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want missing skills error")
	}
}

func TestCatalogAcceptsAllBuiltInTargets(t *testing.T) {
	catalog := Catalog{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: "personal"},
		Skills: map[string]Skill{
			"shared": {
				Source: Source{Type: "local", Path: "skills/shared"},
				Targets: map[string]TargetConfig{
					"codex":    {},
					"claude":   {},
					"pi":       {},
					"opencode": {},
				},
			},
		},
	}
	if err := catalog.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestCatalogAcceptsSupportedInstructionsTargets(t *testing.T) {
	catalog := Catalog{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: "personal"},
		Skills:     map[string]Skill{},
		Instructions: map[string]Instruction{
			"common": {
				Sources: []string{"instructions/common.md"},
				Targets: map[string]TargetConfig{"codex": {}, "pi": {}, "opencode": {}},
			},
		},
	}
	if err := catalog.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	catalog.Instructions["common"] = Instruction{
		Sources: []string{"instructions/common.md"},
		Targets: map[string]TargetConfig{"claude": {}},
	}
	if err := catalog.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want unsupported Claude Instructions target")
	}
}

func TestCatalogAcceptsCodexStdioMCPServer(t *testing.T) {
	catalog := Catalog{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: "personal"},
		Skills:     map[string]Skill{},
		MCPServers: map[string]MCPServer{
			"github": {
				Transport: "stdio",
				Command:   MCPCommand{Executable: "github-mcp-server", Args: []string{"stdio"}},
				Environment: map[string]MCPEnvironmentReference{
					"GITHUB_TOKEN": {From: "env", Name: "GITHUB_TOKEN"},
				},
				Targets: map[string]TargetConfig{"codex": {}},
			},
		},
	}
	if err := catalog.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestCatalogRejectsUnsafeMCPDeclarations(t *testing.T) {
	tests := []struct {
		name   string
		server MCPServer
	}{
		{name: "transport", server: MCPServer{Transport: "http", Command: MCPCommand{Executable: "server"}, Targets: map[string]TargetConfig{"codex": {}}}},
		{name: "shell command", server: MCPServer{Transport: "stdio", Command: MCPCommand{Executable: "server | sh"}, Targets: map[string]TargetConfig{"codex": {}}}},
		{name: "renamed env", server: MCPServer{Transport: "stdio", Command: MCPCommand{Executable: "server"}, Environment: map[string]MCPEnvironmentReference{"TOKEN": {From: "env", Name: "OTHER_TOKEN"}}, Targets: map[string]TargetConfig{"codex": {}}}},
		{name: "unsupported target", server: MCPServer{Transport: "stdio", Command: MCPCommand{Executable: "server"}, Targets: map[string]TargetConfig{"pi": {}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			catalog := Catalog{APIVersion: APIVersion, Kind: Kind, Metadata: Metadata{Name: "personal"}, Skills: map[string]Skill{}, MCPServers: map[string]MCPServer{"server": test.server}}
			if err := catalog.Validate(); err == nil {
				t.Fatal("Validate() error = nil")
			}
		})
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
