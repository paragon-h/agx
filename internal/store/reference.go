package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const referenceVersion = 1

type Reference struct {
	Version  int      `json:"version"`
	Lockfile string   `json:"lockfile"`
	Digests  []string `json:"digests"`
}

type ReferenceRecord struct {
	Manifest string
	Reference
}

func SaveReference(lockfile string, digests []string) error {
	absolute, err := filepath.Abs(lockfile)
	if err != nil {
		return err
	}
	value := Reference{Version: referenceVersion, Lockfile: filepath.Clean(absolute), Digests: uniqueDigests(digests)}
	if err := value.Validate(); err != nil {
		return err
	}
	path, err := referencePath(value.Lockfile)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	file, err := os.CreateTemp(filepath.Dir(path), ".agx-reference-*")
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

func References() ([]ReferenceRecord, error) {
	root, err := Root()
	if err != nil {
		return nil, err
	}
	directory := filepath.Join(root, "refs", "locks")
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return []ReferenceRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	records := make([]ReferenceRecord, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".agx-reference-") {
			continue
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			return nil, fmt.Errorf("unexpected Store reference entry %q", entry.Name())
		}
		path := filepath.Join(directory, entry.Name())
		value, err := loadReference(path)
		if err != nil {
			return nil, err
		}
		wantPath, err := referencePath(value.Lockfile)
		if err != nil {
			return nil, err
		}
		if filepath.Clean(path) != filepath.Clean(wantPath) {
			return nil, fmt.Errorf("Store reference %s has an unexpected filename", path)
		}
		records = append(records, ReferenceRecord{Manifest: path, Reference: value})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Lockfile < records[j].Lockfile })
	return records, nil
}

func RemoveReference(lockfile string) error {
	path, err := referencePath(lockfile)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (r Reference) Validate() error {
	if r.Version != referenceVersion {
		return fmt.Errorf("Store reference version must be %d", referenceVersion)
	}
	if !filepath.IsAbs(r.Lockfile) || filepath.Clean(r.Lockfile) != r.Lockfile {
		return errors.New("Store reference lockfile must be a clean absolute path")
	}
	previous := ""
	for _, digest := range r.Digests {
		if _, err := digestHex(digest); err != nil {
			return err
		}
		if digest <= previous {
			return errors.New("Store reference digests must be unique and sorted")
		}
		previous = digest
	}
	return nil
}

func loadReference(path string) (Reference, error) {
	file, err := os.Open(path)
	if err != nil {
		return Reference{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var value Reference
	if err := decoder.Decode(&value); err != nil {
		return Reference{}, fmt.Errorf("decode Store reference %s: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Reference{}, fmt.Errorf("Store reference %s must contain exactly one JSON value", path)
		}
		return Reference{}, fmt.Errorf("decode trailing Store reference %s: %w", path, err)
	}
	if err := value.Validate(); err != nil {
		return Reference{}, fmt.Errorf("validate Store reference %s: %w", path, err)
	}
	return value, nil
}

func referencePath(lockfile string) (string, error) {
	absolute, err := filepath.Abs(lockfile)
	if err != nil {
		return "", err
	}
	cleaned := filepath.Clean(absolute)
	sum := sha256.Sum256([]byte(cleaned))
	root, err := Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "refs", "locks", hex.EncodeToString(sum[:])+".json"), nil
}

func uniqueDigests(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
