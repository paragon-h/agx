package pi

import (
	"context"
	"path/filepath"

	"github.com/paragon-h/agx/internal/adapters"
)

type Adapter struct{}

func New() Adapter { return Adapter{} }

func (Adapter) Name() string { return "pi" }

func (Adapter) Detect(context.Context) (adapters.Detection, error) {
	return adapters.DetectExecutable("pi"), nil
}

func (Adapter) ResolvePaths(context.Context) (adapters.Paths, error) {
	home, err := adapters.ResolveHome("PI_CODING_AGENT_DIR", ".pi/agent")
	if err != nil {
		return adapters.Paths{}, err
	}
	return adapters.Paths{
		SkillsDir:        filepath.Join(home, "skills"),
		InstructionsFile: filepath.Join(home, "AGENTS.md"),
	}, nil
}
