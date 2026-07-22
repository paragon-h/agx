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

func TestCatalogRejectsEscapingLocalPath(t *testing.T) {
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

func TestCatalogRejectsPortablePathEscape(t *testing.T) {
	for _, sourcePath := range []string{`..\\outside`, `C:\\outside`, `/outside`} {
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
