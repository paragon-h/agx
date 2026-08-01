package claude

import (
	"context"
	"path/filepath"

	"github.com/alanhuangch/agx/internal/adapters"
)

type Adapter struct{}

func New() Adapter { return Adapter{} }

func (Adapter) Name() string { return "claude" }

func (Adapter) Detect(context.Context) (adapters.Detection, error) {
	return adapters.DetectExecutable("claude"), nil
}

func (Adapter) ResolvePaths(context.Context) (adapters.Paths, error) {
	home, err := adapters.ResolveHome("CLAUDE_CONFIG_DIR", ".claude")
	if err != nil {
		return adapters.Paths{}, err
	}
	return adapters.Paths{
		SkillsDir:        filepath.Join(home, "skills"),
		InstructionsFile: filepath.Join(home, "CLAUDE.md"),
	}, nil
}
