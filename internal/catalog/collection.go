package catalog

import (
	"fmt"
	"sort"
)

type Collection struct {
	Documents map[string]Document
	Names     []string
}

type Resource struct {
	CatalogName   string
	Name          string
	QualifiedName string
	Document      Document
	Skill         Skill
}

type InstructionResource struct {
	CatalogName   string
	Name          string
	QualifiedName string
	Document      Document
	Instruction   Instruction
}

type MCPServerResource struct {
	CatalogName   string
	Name          string
	QualifiedName string
	Document      Document
	MCPServer     MCPServer
}

type Selection struct {
	Profile      string
	Targets      []string
	Resources    []Resource
	Instructions []InstructionResource
	MCPServers   []MCPServerResource
}

func NewCollection(documents []Document) (Collection, error) {
	collection := Collection{Documents: make(map[string]Document, len(documents))}
	for _, document := range documents {
		name := document.Catalog.Metadata.Name
		if _, exists := collection.Documents[name]; exists {
			return Collection{}, fmt.Errorf("catalog %q is included more than once", name)
		}
		collection.Documents[name] = document
		collection.Names = append(collection.Names, name)
	}
	sort.Strings(collection.Names)
	return collection, nil
}

func (c Collection) Resources() []Resource {
	resources := make([]Resource, 0)
	for _, catalogName := range c.Names {
		document := c.Documents[catalogName]
		names := make([]string, 0, len(document.Catalog.Skills))
		for name := range document.Catalog.Skills {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			resources = append(resources, Resource{
				CatalogName:   catalogName,
				Name:          name,
				QualifiedName: QualifiedName(catalogName, name),
				Document:      document,
				Skill:         document.Catalog.Skills[name],
			})
		}
	}
	return resources
}

func (c Collection) SelectProfile(reference string) (Selection, error) {
	resources := c.Resources()
	if reference == "" {
		return Selection{Resources: resources, Instructions: c.InstructionResources(), MCPServers: c.MCPServerResources()}, nil
	}
	owner, profileName, err := c.resolveProfile(reference)
	if err != nil {
		return Selection{}, err
	}
	profile := c.Documents[owner].Catalog.Profiles[profileName]
	byName := make(map[string]Resource, len(resources))
	for _, resource := range resources {
		byName[resource.QualifiedName] = resource
	}

	included := make(map[string]struct{}, len(resources))
	if len(profile.Skills.Include) == 0 {
		for name := range byName {
			included[name] = struct{}{}
		}
	} else {
		for _, name := range profile.Skills.Include {
			qualified, qualifyErr := qualifyResourceReference(owner, name)
			if qualifyErr != nil {
				return Selection{}, qualifyErr
			}
			if _, exists := byName[qualified]; !exists {
				return Selection{}, fmt.Errorf("profile %q references unavailable skill %q", QualifiedName(owner, profileName), name)
			}
			included[qualified] = struct{}{}
		}
	}
	for _, name := range profile.Skills.Exclude {
		qualified, qualifyErr := qualifyResourceReference(owner, name)
		if qualifyErr != nil {
			return Selection{}, qualifyErr
		}
		if _, exists := byName[qualified]; !exists {
			return Selection{}, fmt.Errorf("profile %q references unavailable skill %q", QualifiedName(owner, profileName), name)
		}
		delete(included, qualified)
	}

	targets := make(map[string]struct{}, len(profile.Targets))
	for _, target := range profile.Targets {
		targets[target] = struct{}{}
	}
	selected := Selection{Profile: QualifiedName(owner, profileName), Targets: append([]string(nil), profile.Targets...)}
	for _, resource := range resources {
		if _, exists := included[resource.QualifiedName]; !exists {
			continue
		}
		if len(targets) != 0 {
			filtered := make(map[string]TargetConfig, len(resource.Skill.Targets))
			for target, config := range resource.Skill.Targets {
				if _, exists := targets[target]; exists {
					filtered[target] = config
				}
			}
			resource.Skill.Targets = filtered
		}
		if hasEnabledTarget(resource.Skill.Targets) {
			selected.Resources = append(selected.Resources, resource)
		}
	}
	for _, resource := range c.InstructionResources() {
		if len(targets) != 0 {
			filtered := make(map[string]TargetConfig, len(resource.Instruction.Targets))
			for target, config := range resource.Instruction.Targets {
				if _, exists := targets[target]; exists {
					filtered[target] = config
				}
			}
			resource.Instruction.Targets = filtered
		}
		if hasEnabledTarget(resource.Instruction.Targets) {
			selected.Instructions = append(selected.Instructions, resource)
		}
	}
	for _, resource := range c.MCPServerResources() {
		if len(targets) != 0 {
			filtered := make(map[string]TargetConfig, len(resource.MCPServer.Targets))
			for target, config := range resource.MCPServer.Targets {
				if _, exists := targets[target]; exists {
					filtered[target] = config
				}
			}
			resource.MCPServer.Targets = filtered
		}
		if hasEnabledTarget(resource.MCPServer.Targets) {
			selected.MCPServers = append(selected.MCPServers, resource)
		}
	}
	return selected, nil
}

func (c Collection) InstructionResources() []InstructionResource {
	resources := make([]InstructionResource, 0)
	for _, catalogName := range c.Names {
		document := c.Documents[catalogName]
		names := make([]string, 0, len(document.Catalog.Instructions))
		for name := range document.Catalog.Instructions {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			resources = append(resources, InstructionResource{
				CatalogName:   catalogName,
				Name:          name,
				QualifiedName: QualifiedName(catalogName, name),
				Document:      document,
				Instruction:   document.Catalog.Instructions[name],
			})
		}
	}
	return resources
}

func (c Collection) MCPServerResources() []MCPServerResource {
	resources := make([]MCPServerResource, 0)
	for _, catalogName := range c.Names {
		document := c.Documents[catalogName]
		names := make([]string, 0, len(document.Catalog.MCPServers))
		for name := range document.Catalog.MCPServers {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			resources = append(resources, MCPServerResource{
				CatalogName:   catalogName,
				Name:          name,
				QualifiedName: QualifiedName(catalogName, name),
				Document:      document,
				MCPServer:     document.Catalog.MCPServers[name],
			})
		}
	}
	return resources
}

func (c Collection) resolveProfile(reference string) (string, string, error) {
	catalogName, profileName, qualified, err := ParseQualifiedName(reference)
	if err != nil {
		return "", "", fmt.Errorf("profile %q is invalid", reference)
	}
	if qualified {
		document, exists := c.Documents[catalogName]
		if !exists {
			return "", "", fmt.Errorf("profile %q belongs to an unavailable catalog", reference)
		}
		if _, exists := document.Catalog.Profiles[profileName]; !exists {
			return "", "", fmt.Errorf("profile %q is not declared", reference)
		}
		return catalogName, profileName, nil
	}
	found := ""
	for _, name := range c.Names {
		if _, exists := c.Documents[name].Catalog.Profiles[profileName]; !exists {
			continue
		}
		if found != "" {
			return "", "", fmt.Errorf("profile name %q is ambiguous; use a qualified name", profileName)
		}
		found = name
	}
	if found == "" {
		return "", "", fmt.Errorf("profile %q is not declared", profileName)
	}
	return found, profileName, nil
}

func qualifyResourceReference(owner, reference string) (string, error) {
	catalogName, resourceName, qualified, err := ParseQualifiedName(reference)
	if err != nil {
		return "", fmt.Errorf("skill reference %q is invalid", reference)
	}
	if !qualified {
		catalogName = owner
	}
	return QualifiedName(catalogName, resourceName), nil
}
