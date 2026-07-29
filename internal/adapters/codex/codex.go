package codex

import (
	"context"
	"path/filepath"

	"github.com/alanhuangch/agx/internal/adapters"
)

type Adapter struct{}

func New() Adapter { return Adapter{} }

func (Adapter) Name() string { return "codex" }

func (Adapter) Detect(context.Context) (adapters.Detection, error) {
	return adapters.DetectExecutable("codex"), nil
}

func (Adapter) ResolvePaths(context.Context) (adapters.Paths, error) {
	home, err := adapters.ResolveHome("CODEX_HOME", ".codex")
	if err != nil {
		return adapters.Paths{}, err
	}
	return adapters.Paths{
		SkillsDir:                filepath.Join(home, "skills"),
		InstructionsFile:         filepath.Join(home, "AGENTS.md"),
		InstructionsOverrideFile: filepath.Join(home, "AGENTS.override.md"),
		ConfigFile:               filepath.Join(home, "config.toml"),
	}, nil
}
