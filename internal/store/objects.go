package store

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

type Object struct {
	Digest string
	Path   string
	Size   int64
}

type ScanIssue struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

func Objects() ([]Object, []ScanIssue, error) {
	root, err := Root()
	if err != nil {
		return nil, nil, err
	}
	directory := filepath.Join(root, "objects", "sha256")
	shards, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return []Object{}, []ScanIssue{}, nil
	}
	if err != nil {
		return nil, nil, err
	}
	objects := make([]Object, 0)
	issues := make([]ScanIssue, 0)
	for _, shard := range shards {
		shardPath := filepath.Join(directory, shard.Name())
		if !shard.IsDir() || !validHex(shard.Name(), 2) {
			issues = append(issues, ScanIssue{Path: shardPath, Error: "invalid sha256 shard"})
			continue
		}
		entries, err := os.ReadDir(shardPath)
		if err != nil {
			issues = append(issues, ScanIssue{Path: shardPath, Error: err.Error()})
			continue
		}
		for _, entry := range entries {
			path := filepath.Join(shardPath, entry.Name())
			if !entry.IsDir() || !validHex(entry.Name(), 62) {
				issues = append(issues, ScanIssue{Path: path, Error: "invalid sha256 object"})
				continue
			}
			size, err := directorySize(path)
			if err != nil {
				issues = append(issues, ScanIssue{Path: path, Error: err.Error()})
				continue
			}
			objects = append(objects, Object{Digest: "sha256:" + shard.Name() + entry.Name(), Path: path, Size: size})
		}
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].Digest < objects[j].Digest })
	sort.Slice(issues, func(i, j int) bool { return issues[i].Path < issues[j].Path })
	return objects, issues, nil
}

func Remove(digest string) error {
	path, err := ObjectPath(digest)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	_ = os.Remove(filepath.Dir(path))
	return nil
}

func directorySize(root string) (int64, error) {
	var size int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic link is not supported: %s", path)
		}
		if info.Mode().IsRegular() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

func validHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}
