package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

type Generation struct {
	ID             string  `json:"id"`
	CreatedAt      string  `json:"createdAt"`
	CatalogDigest  string  `json:"catalogDigest"`
	LockfileDigest string  `json:"lockfileDigest"`
	PreviousID     string  `json:"previousId,omitempty"`
	Entries        []Entry `json:"entries"`
}

type Entry struct {
	Target        string `json:"target"`
	Skill         string `json:"skill"`
	Path          string `json:"path"`
	ContentDigest string `json:"contentDigest"`
}

func Current() (*Generation, error) {
	root, err := Root()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(root, "current.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var generation Generation
	if err := json.Unmarshal(data, &generation); err != nil {
		return nil, fmt.Errorf("decode current generation: %w", err)
	}
	if err := generation.Validate(); err != nil {
		return nil, fmt.Errorf("validate current generation: %w", err)
	}
	return &generation, nil
}

func Save(generation Generation) error {
	if err := generation.Validate(); err != nil {
		return err
	}
	root, err := Root()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, "generations"), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(generation, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := atomicWrite(filepath.Join(root, "generations", generation.ID+".json"), data); err != nil {
		return fmt.Errorf("write generation: %w", err)
	}
	if err := atomicWrite(filepath.Join(root, "current.json"), data); err != nil {
		return fmt.Errorf("write current generation: %w", err)
	}
	return nil
}

func AcquireApplyLock() (func() error, error) {
	root, err := Root()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(root, "apply.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return nil, errors.New("another agx apply operation is active")
		}
		return nil, err
	}
	if _, err := fmt.Fprintf(file, "%d\n", os.Getpid()); err != nil {
		file.Close()
		os.Remove(path)
		return nil, err
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return nil, err
	}
	return func() error { return os.Remove(path) }, nil
}

func Root() (string, error) {
	if override := os.Getenv("AGX_STATE_HOME"); override != "" {
		if !filepath.IsAbs(override) {
			return "", errors.New("AGX_STATE_HOME must be an absolute path")
		}
		return filepath.Clean(override), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "agx", "state"), nil
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = os.Getenv("APPDATA")
		}
		if base == "" {
			base = home
		}
		return filepath.Join(base, "agx", "state"), nil
	default:
		base := os.Getenv("XDG_STATE_HOME")
		if base == "" {
			base = filepath.Join(home, ".local", "state")
		}
		return filepath.Join(base, "agx"), nil
	}
}

func (g Generation) Validate() error {
	if g.ID == "" || strings.ContainsAny(g.ID, `/\\`) {
		return errors.New("generation ID is invalid")
	}
	if _, err := time.Parse(time.RFC3339, g.CreatedAt); err != nil {
		return errors.New("generation createdAt must be RFC 3339")
	}
	if !validDigest(g.CatalogDigest) || !validDigest(g.LockfileDigest) {
		return errors.New("generation digests must be sha256 digests")
	}
	seenPaths := make(map[string]struct{}, len(g.Entries))
	for i, entry := range g.Entries {
		if entry.Target == "" || entry.Skill == "" || !filepath.IsAbs(entry.Path) || !validDigest(entry.ContentDigest) {
			return fmt.Errorf("generation entry %d is invalid", i)
		}
		cleaned := filepath.Clean(entry.Path)
		if _, exists := seenPaths[cleaned]; exists {
			return fmt.Errorf("generation entry %d duplicates path %s", i, cleaned)
		}
		seenPaths[cleaned] = struct{}{}
	}
	return nil
}

func (g Generation) ManagedByPath() map[string]Entry {
	managed := make(map[string]Entry, len(g.Entries))
	for _, entry := range g.Entries {
		managed[filepath.Clean(entry.Path)] = entry
	}
	return managed
}

func SortEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Path != entries[j].Path {
			return entries[i].Path < entries[j].Path
		}
		return entries[i].Skill < entries[j].Skill
	})
}

func atomicWrite(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".agx-state-*")
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
	return os.Rename(temporary, path)
}

func validDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, char := range value[len("sha256:"):] {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}
