package lockfile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func Load(path string) (Lockfile, error) {
	file, err := os.Open(path)
	if err != nil {
		return Lockfile{}, err
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	var value Lockfile
	if err := decoder.Decode(&value); err != nil {
		return Lockfile{}, fmt.Errorf("decode lockfile: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Lockfile{}, errors.New("lockfile must contain exactly one YAML document")
		}
		return Lockfile{}, fmt.Errorf("decode trailing lockfile document: %w", err)
	}
	if err := value.Validate(); err != nil {
		return Lockfile{}, fmt.Errorf("validate lockfile: %w", err)
	}
	return value, nil
}

func Write(path string, value Lockfile) error {
	if err := value.Validate(); err != nil {
		return fmt.Errorf("validate lockfile before writing: %w", err)
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".agx-lock-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	encoder := yaml.NewEncoder(temporary)
	encoder.SetIndent(2)
	if err := encoder.Encode(value); err != nil {
		encoder.Close()
		temporary.Close()
		return err
	}
	if err := encoder.Close(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return nil
}
