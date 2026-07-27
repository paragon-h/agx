package filetree

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func Copy(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("source root must be a regular directory")
	}
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("destination already exists: %s", destination)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source {
			return nil
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic link %q is not supported", relative)
		}
		if entry.IsDir() {
			return os.Mkdir(target, 0o755)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("special file %q is not supported", relative)
		}
		permissions := os.FileMode(0o644)
		if info.Mode()&0o111 != 0 {
			permissions = 0o755
		}
		return copyFile(path, target, permissions)
	})
}

// CopyPath copies a regular directory or file. An empty kind is treated as a
// directory for compatibility with state written before file targets existed.
func CopyPath(source, destination, kind string) error {
	switch kind {
	case "", "directory":
		return Copy(source, destination)
	case "file":
		info, err := os.Lstat(source)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("source must be a regular file")
		}
		if _, err := os.Lstat(destination); err == nil {
			return fmt.Errorf("destination already exists: %s", destination)
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		permissions := os.FileMode(0o644)
		if info.Mode()&0o111 != 0 {
			permissions = 0o755
		}
		return copyFile(source, destination, permissions)
	default:
		return fmt.Errorf("unsupported content kind %q", kind)
	}
}

func copyFile(source, destination string, permissions os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, permissions)
	if err != nil {
		return err
	}
	if err := output.Chmod(permissions); err != nil {
		output.Close()
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
