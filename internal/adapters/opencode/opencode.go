package opencode

import (
	"context"

	"github.com/paragon-h/agx/internal/adapters"
)

type Adapter struct{}

func New() Adapter { return Adapter{} }

func (Adapter) Name() string { return "opencode" }

func (Adapter) Detect(context.Context) (adapters.Detection, error) {
	return adapters.DetectExecutable("opencode"), nil
}

func (Adapter) ResolvePaths(context.Context) (adapters.Paths, error) {
	path, err := adapters.XDGSkillsPath("opencode")
	if err != nil {
		return adapters.Paths{}, err
	}
	return adapters.Paths{SkillsDir: path}, nil
}
