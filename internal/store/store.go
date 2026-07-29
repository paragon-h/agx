package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alanhuangch/agx/internal/contenthash"
	"github.com/alanhuangch/agx/internal/filetree"
	"github.com/alanhuangch/agx/internal/state"
)

var (
	ErrNotFound = errors.New("store object not found")
	ErrCorrupt  = errors.New("store object is corrupt")
)

func Root() (string, error) {
	if override := os.Getenv("AGX_STORE_HOME"); override != "" {
		if !filepath.IsAbs(override) {
			return "", errors.New("AGX_STORE_HOME must be an absolute path")
		}
		return filepath.Clean(override), nil
	}
	root, err := state.Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "store"), nil
}

func Put(source, digest string) error {
	path, err := ObjectPath(digest)
	if err != nil {
		return err
	}
	actual, err := contenthash.Directory(source)
	if err != nil {
		return err
	}
	if actual != digest {
		return fmt.Errorf("source digest %s does not match requested Store object %s", actual, digest)
	}
	if err := Verify(digest); err == nil {
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	temporary, err := os.MkdirTemp(parent, ".agx-object-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	content := filepath.Join(temporary, "content")
	if err := filetree.Copy(source, content); err != nil {
		return err
	}
	copiedDigest, err := contenthash.Directory(content)
	if err != nil {
		return err
	}
	if copiedDigest != digest {
		return fmt.Errorf("copied Store object digest %s does not match %s", copiedDigest, digest)
	}
	if err := os.Rename(content, path); err != nil {
		if verifyErr := Verify(digest); verifyErr == nil {
			return nil
		}
		return err
	}
	return nil
}

func Materialize(digest, destination string) error {
	path, err := ObjectPath(digest)
	if err != nil {
		return err
	}
	if err := Verify(digest); err != nil {
		return err
	}
	if err := filetree.Copy(path, destination); err != nil {
		return err
	}
	actual, err := contenthash.Directory(destination)
	if err != nil {
		return err
	}
	if actual != digest {
		return fmt.Errorf("%w: materialized digest %s does not match %s", ErrCorrupt, actual, digest)
	}
	return nil
}

func Verify(digest string) error {
	path, err := ObjectPath(digest)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return fmt.Errorf("%w: %s", ErrNotFound, digest)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s is not a regular directory", ErrCorrupt, digest)
	}
	actual, err := contenthash.Directory(path)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if actual != digest {
		return fmt.Errorf("%w: object %s has digest %s", ErrCorrupt, digest, actual)
	}
	return nil
}

func ObjectPath(digest string) (string, error) {
	hex, err := digestHex(digest)
	if err != nil {
		return "", err
	}
	root, err := Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "objects", "sha256", hex[:2], hex[2:]), nil
}

func digestHex(digest string) (string, error) {
	if len(digest) != len("sha256:")+64 || !strings.HasPrefix(digest, "sha256:") {
		return "", errors.New("Store digest must be a sha256 digest")
	}
	hex := digest[len("sha256:"):]
	for _, char := range hex {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return "", errors.New("Store digest must be a lowercase sha256 digest")
		}
	}
	return hex, nil
}
