package catalog

import "fmt"

// SelectProfile returns a Catalog containing only the Skills and targets selected
// by profileName. An empty profile name preserves the complete Catalog.
func (c Catalog) SelectProfile(profileName string) (Catalog, error) {
	if profileName == "" {
		return c, nil
	}
	profile, exists := c.Profiles[profileName]
	if !exists {
		return Catalog{}, fmt.Errorf("profile %q is not declared in catalog %q", profileName, c.Metadata.Name)
	}

	included := make(map[string]struct{}, len(c.Skills))
	if len(profile.Skills.Include) == 0 {
		for name := range c.Skills {
			included[name] = struct{}{}
		}
	} else {
		for _, name := range profile.Skills.Include {
			included[name] = struct{}{}
		}
	}
	for _, name := range profile.Skills.Exclude {
		delete(included, name)
	}

	targetFilter := make(map[string]struct{}, len(profile.Targets))
	for _, target := range profile.Targets {
		targetFilter[target] = struct{}{}
	}

	selected := c
	selected.Skills = make(map[string]Skill, len(included))
	for name := range included {
		skill := c.Skills[name]
		if len(targetFilter) != 0 {
			targets := make(map[string]TargetConfig, len(skill.Targets))
			for target, config := range skill.Targets {
				if _, enabled := targetFilter[target]; enabled {
					targets[target] = config
				}
			}
			skill.Targets = targets
		}
		if hasEnabledTarget(skill.Targets) {
			selected.Skills[name] = skill
		}
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
