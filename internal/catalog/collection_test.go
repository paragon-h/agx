package catalog

import (
	"strings"
	"testing"
)

func TestCollectionSelectsCrossCatalogProfile(t *testing.T) {
	personal := profileTestCatalog()
	personal.Profiles = map[string]Profile{
		"work": {
			Skills:  ProfileSkills{Include: []string{"review", "work/deploy"}},
			Targets: []string{"codex"},
		},
	}
	work := profileTestCatalog()
	work.Metadata.Name = "work"
	delete(work.Skills, "review")
	collection, err := NewCollection([]Document{{Catalog: work}, {Catalog: personal}})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := collection.SelectProfile("personal/work")
	if err != nil {
		t.Fatal(err)
	}
	if selection.Profile != "personal/work" || len(selection.Resources) != 2 {
		t.Fatalf("selection = %#v", selection)
	}
	if selection.Resources[0].QualifiedName != "personal/review" || selection.Resources[1].QualifiedName != "work/deploy" {
		t.Fatalf("resources = %#v", selection.Resources)
	}
}

func TestCollectionRejectsAmbiguousProfileName(t *testing.T) {
	personal := profileTestCatalog()
	personal.Profiles = map[string]Profile{"default": {}}
	work := profileTestCatalog()
	work.Metadata.Name = "work"
	work.Profiles = map[string]Profile{"default": {}}
	collection, err := NewCollection([]Document{{Catalog: personal}, {Catalog: work}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := collection.SelectProfile("default"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("SelectProfile() error = %v, want ambiguity", err)
	}
}

func TestCollectionRejectsUnavailableCrossCatalogReference(t *testing.T) {
	personal := profileTestCatalog()
	personal.Profiles = map[string]Profile{
		"work": {Skills: ProfileSkills{Include: []string{"missing/deploy"}}},
	}
	collection, err := NewCollection([]Document{{Catalog: personal}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := collection.SelectProfile("work"); err == nil || !strings.Contains(err.Error(), "unavailable skill") {
		t.Fatalf("SelectProfile() error = %v, want unavailable skill", err)
	}
}

func TestCollectionResourcesAreDeterministic(t *testing.T) {
	personal := profileTestCatalog()
	work := profileTestCatalog()
	work.Metadata.Name = "work"
	collection, err := NewCollection([]Document{{Catalog: work}, {Catalog: personal}})
	if err != nil {
		t.Fatal(err)
	}
	resources := collection.Resources()
	want := []string{"personal/deploy", "personal/review", "work/deploy", "work/review"}
	if len(resources) != len(want) {
		t.Fatalf("resources = %#v", resources)
	}
	for i := range want {
		if resources[i].QualifiedName != want[i] {
			t.Fatalf("resource %d = %q, want %q", i, resources[i].QualifiedName, want[i])
		}
	}
}
