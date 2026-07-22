package adapters

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type Adapter interface {
	Name() string
	Detect(context.Context) (Detection, error)
	ResolvePaths(context.Context) (Paths, error)
}

type Detection struct {
	Installed  bool
	Executable string
}

type Paths struct {
	SkillsDir string
}

func DetectExecutable(name string) Detection {
	path, err := exec.LookPath(name)
	if err != nil {
		return Detection{}
	}
	return Detection{Installed: true, Executable: path}
}

func ResolveHome(envKey, defaultDirectory string) (string, error) {
	home := os.Getenv(envKey)
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve user home: %w", err)
		}
		home = filepath.Join(home, defaultDirectory)
	} else if !filepath.IsAbs(home) {
		return "", errors.New(envKey + " must be an absolute path")
	}
	return filepath.Clean(home), nil
}

func SkillsPath(envKey, defaultDirectory string) (string, error) {
	home, err := ResolveHome(envKey, defaultDirectory)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "skills"), nil
}
