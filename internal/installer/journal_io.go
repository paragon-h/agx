package installer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/paragon-h/agx/internal/state"
)

func SaveJournal(journal Journal) error {
	if err := journal.Validate(); err != nil {
		return err
	}
	path, err := journalPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	file, err := os.CreateTemp(filepath.Dir(path), ".agx-journal-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	return nil
}

func LoadJournal() (*Journal, error) {
	path, err := journalPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var journal Journal
	if err := json.Unmarshal(data, &journal); err != nil {
		return nil, fmt.Errorf("decode transaction journal: %w", err)
	}
	if err := journal.Validate(); err != nil {
		return nil, fmt.Errorf("validate transaction journal: %w", err)
	}
	return &journal, nil
}

func DeleteJournal() error {
	path, err := journalPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func journalPath() (string, error) {
	root, err := state.Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "transactions", "current.json"), nil
}
