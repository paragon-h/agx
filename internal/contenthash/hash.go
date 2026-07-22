package contenthash

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return format(hash.Sum(nil)), nil
}

// Directory hashes portable relative paths, executable bits, and file content.
// Milestone 1 rejects links and other special files instead of following them.
func Directory(root string) (string, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("source root must not be a symbolic link")
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", root)
	}

	hash := sha256.New()
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}

		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)

		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic link %q is not supported in Milestone 1", relative)
		}
		if entry.IsDir() {
			if _, err := io.WriteString(hash, "AGX-DIR-V1\x00"); err != nil {
				return err
			}
			if err := binary.Write(hash, binary.BigEndian, uint64(len(relative))); err != nil {
				return err
			}
			_, err := io.WriteString(hash, relative)
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("special file %q is not supported", relative)
		}

		if _, err := io.WriteString(hash, "AGX-FILE-V1\x00"); err != nil {
			return err
		}
		if err := binary.Write(hash, binary.BigEndian, uint64(len(relative))); err != nil {
			return err
		}
		if _, err := io.WriteString(hash, relative); err != nil {
			return err
		}
		if info.Mode()&0o111 != 0 {
			if _, err := hash.Write([]byte{1}); err != nil {
				return err
			}
		} else if _, err := hash.Write([]byte{0}); err != nil {
			return err
		}
		if err := binary.Write(hash, binary.BigEndian, uint64(info.Size())); err != nil {
			return err
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return format(hash.Sum(nil)), nil
}

func Equal(left, right string) error {
	if left != right {
		return errors.New("content digest mismatch")
	}
	return nil
}

func format(sum []byte) string {
	return "sha256:" + hex.EncodeToString(sum)
}
