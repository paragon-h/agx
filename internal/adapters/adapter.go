package adapters

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	SkillsDir                string
	InstructionsFile         string
	InstructionsOverrideFile string
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
	} else {
		var err error
		home, err = expandHomePath(home)
		if err != nil {
			return "", fmt.Errorf("%s: %w", envKey, err)
		}
		if !filepath.IsAbs(home) {
			return "", errors.New(envKey + " must be an absolute path or use ~/ for the user home")
		}
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

func XDGSkillsPath(application string) (string, error) {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve user home: %w", err)
		}
		configHome = filepath.Join(home, ".config")
	} else {
		var err error
		configHome, err = expandHomePath(configHome)
		if err != nil {
			return "", fmt.Errorf("XDG_CONFIG_HOME: %w", err)
		}
		if !filepath.IsAbs(configHome) {
			return "", errors.New("XDG_CONFIG_HOME must be an absolute path or use ~/ for the user home")
		}
	}
	return filepath.Join(filepath.Clean(configHome), application, "skills"), nil
}

func expandHomePath(value string) (string, error) {
	if value != "~" && !strings.HasPrefix(value, "~/") && !strings.HasPrefix(value, `~\`) {
		return value, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	if value == "~" {
		return home, nil
	}
	relative := strings.TrimLeft(value[1:], `/\`)
	return filepath.Join(home, filepath.FromSlash(strings.ReplaceAll(relative, `\`, "/"))), nil
}
