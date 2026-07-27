package catalog

// SelectProfile returns a Catalog containing only the Skills and targets selected
// by profileName. An empty profile name preserves the complete Catalog.
func (c Catalog) SelectProfile(profileName string) (Catalog, error) {
	if profileName == "" {
		return c, nil
	}
	document := Document{Catalog: c}
	collection, err := NewCollection([]Document{document})
	if err != nil {
		return Catalog{}, err
	}
	selection, err := collection.SelectProfile(profileName)
	if err != nil {
		return Catalog{}, err
	}
	selected := c
	selected.Skills = make(map[string]Skill, len(selection.Resources))
	for _, resource := range selection.Resources {
		selected.Skills[resource.Name] = resource.Skill
	}
	selected.Instructions = make(map[string]Instruction, len(selection.Instructions))
	for _, resource := range selection.Instructions {
		selected.Instructions[resource.Name] = resource.Instruction
	}
	selected.MCPServers = make(map[string]MCPServer, len(selection.MCPServers))
	for _, resource := range selection.MCPServers {
		selected.MCPServers[resource.Name] = resource.MCPServer
	}
	return selected, nil
}

func hasEnabledTarget(targets map[string]TargetConfig) bool {
	for _, config := range targets {
		if config.Enabled == nil || *config.Enabled {
			return true
		}
	}
	return false
}
