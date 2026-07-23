package pi

import (
	"context"

	"github.com/paragon-h/agx/internal/adapters"
)

type Adapter struct{}

func New() Adapter { return Adapter{} }

func (Adapter) Name() string { return "pi" }

func (Adapter) Detect(context.Context) (adapters.Detection, error) {
	return adapters.DetectExecutable("pi"), nil
}

func (Adapter) ResolvePaths(context.Context) (adapters.Paths, error) {
	path, err := adapters.SkillsPath("PI_CODING_AGENT_DIR", ".pi/agent")
	if err != nil {
		return adapters.Paths{}, err
	}
	return adapters.Paths{SkillsDir: path}, nil
}
