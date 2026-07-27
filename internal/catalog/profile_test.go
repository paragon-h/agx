package catalog

import (
	"strings"
	"testing"
)

func TestCatalogSelectProfileFiltersSkillsAndTargets(t *testing.T) {
	value := profileTestCatalog()
	value.Profiles = map[string]Profile{
		"work": {
			Skills:  ProfileSkills{Include: []string{"review"}, Exclude: []string{"deploy"}},
			Targets: []string{"claude"},
		},
	}
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}

	selected, err := value.SelectProfile("work")
	if err != nil {
		t.Fatal(err)
	}
	if len(selected.Skills) != 1 {
		t.Fatalf("selected Skills = %#v, want only review", selected.Skills)
	}
	review := selected.Skills["review"]
	if len(review.Targets) != 1 {
		t.Fatalf("selected targets = %#v, want only claude", review.Targets)
	}
	if _, exists := review.Targets["claude"]; !exists {
		t.Fatalf("selected targets = %#v, want claude", review.Targets)
	}
	if len(value.Skills["review"].Targets) != 2 {
		t.Fatal("SelectProfile mutated the original Catalog")
	}
}

func TestCatalogSelectProfileDefaultsToAllSkills(t *testing.T) {
	value := profileTestCatalog()
	value.Profiles = map[string]Profile{
		"without-deploy": {Skills: ProfileSkills{Exclude: []string{"deploy"}}},
	}
	selected, err := value.SelectProfile("without-deploy")
	if err != nil {
		t.Fatal(err)
	}
	if len(selected.Skills) != 1 || selected.Skills["review"].Source.Path == "" {
		t.Fatalf("selected Skills = %#v, want review", selected.Skills)
	}
}

func TestCatalogSelectProfileDropsSkillsWithoutMatchingTargets(t *testing.T) {
	value := profileTestCatalog()
	value.Profiles = map[string]Profile{
		"pi-only": {Targets: []string{"pi"}},
	}
	selected, err := value.SelectProfile("pi-only")
	if err != nil {
		t.Fatal(err)
	}
	if len(selected.Skills) != 0 {
		t.Fatalf("selected Skills = %#v, want empty", selected.Skills)
	}
}

func TestCatalogProfileValidationRejectsInvalidReferences(t *testing.T) {
	tests := []struct {
		name    string
		profile Profile
		want    string
	}{
		{name: "unknown skill", profile: Profile{Skills: ProfileSkills{Include: []string{"missing"}}}, want: "not declared"},
		{name: "duplicate skill", profile: Profile{Skills: ProfileSkills{Include: []string{"review", "review"}}}, want: "duplicated"},
		{name: "overlapping skill", profile: Profile{Skills: ProfileSkills{Include: []string{"review"}, Exclude: []string{"review"}}}, want: "both include and exclude"},
		{name: "unsupported target", profile: Profile{Targets: []string{"unknown"}}, want: "unsupported target"},
		{name: "duplicate target", profile: Profile{Targets: []string{"codex", "codex"}}, want: "duplicated"},
		{name: "empty targets", profile: Profile{Targets: []string{}}, want: "at least one agent"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := profileTestCatalog()
			value.Profiles = map[string]Profile{"work": test.profile}
			err := value.Validate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestCatalogSelectProfileRejectsUnknownProfile(t *testing.T) {
	value := profileTestCatalog()
	if _, err := value.SelectProfile("missing"); err == nil {
		t.Fatal("SelectProfile() error = nil, want unknown profile error")
	}
}

func profileTestCatalog() Catalog {
	return Catalog{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: "personal"},
		Skills: map[string]Skill{
			"review": {
				Source: Source{Type: "local", Path: "skills/review"},
				Targets: map[string]TargetConfig{
					"codex":  {},
					"claude": {},
				},
			},
			"deploy": {
				Source:  Source{Type: "local", Path: "skills/deploy"},
				Targets: map[string]TargetConfig{"codex": {}},
			},
		},
	}
}
