package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/alanhuangch/agx/internal/catalog"
	"github.com/alanhuangch/agx/internal/contenthash"
	"github.com/alanhuangch/agx/internal/filetree"
	"github.com/alanhuangch/agx/internal/lockfile"
	"github.com/alanhuangch/agx/internal/overlay"
	gitresolver "github.com/alanhuangch/agx/internal/resolver/git"
	"github.com/alanhuangch/agx/internal/state"
	"github.com/alanhuangch/agx/internal/store"
)

func storeCurrentSkillSource(ctx context.Context, document catalog.Document, skill catalog.Skill) (lockfile.LockedSource, string, error) {
	switch skill.Source.Type {
	case "local":
		sourcePath, err := document.Resolve(skill.Source.Path)
		if err != nil {
			return lockfile.LockedSource{}, "", err
		}
		digest, err := skillDigest(sourcePath)
		if err != nil {
			return lockfile.LockedSource{}, "", err
		}
		if err := store.Put(sourcePath, digest); err != nil {
			return lockfile.LockedSource{}, "", fmt.Errorf("store local Skill: %w", err)
		}
		return lockfile.LockedSource{Type: "local", Path: skill.Source.Path}, digest, nil
	case "git":
		temporary, err := os.MkdirTemp("", "agx-lock-source-*")
		if err != nil {
			return lockfile.LockedSource{}, "", err
		}
		defer os.RemoveAll(temporary)
		destination := filepath.Join(temporary, "content")
		result, err := gitresolver.New().MaterializeSkill(ctx, gitresolver.Request{
			Repository: skill.Source.Repository,
			Revision:   skill.Source.Revision,
			Path:       skill.Source.Path,
		}, destination)
		if err != nil {
			return lockfile.LockedSource{}, "", err
		}
		if err := store.Put(destination, result.ContentDigest); err != nil {
			return lockfile.LockedSource{}, "", fmt.Errorf("store Git Skill: %w", err)
		}
		return lockfile.LockedSource{
			Type:              "git",
			Repository:        skill.Source.Repository,
			RequestedRevision: skill.Source.Revision,
			ResolvedCommit:    result.ResolvedCommit,
			Path:              skill.Source.Path,
		}, result.ContentDigest, nil
	default:
		return lockfile.LockedSource{}, "", fmt.Errorf("unsupported source type %q", skill.Source.Type)
	}
}

func saveLockStoreReference(path string, value lockfile.Lockfile) error {
	digests := make([]string, 0, len(value.Skills)*2)
	for _, skill := range value.Skills {
		digests = append(digests, skill.ContentDigest)
		if skill.OverlayDigest != "" {
			digests = append(digests, skill.OverlayDigest)
		}
	}
	return store.SaveReference(path, digests)
}

func materializeLockedSource(ctx context.Context, document catalog.Document, locked lockfile.LockedSkill, destination string) error {
	if err := store.Materialize(locked.ContentDigest, destination); err == nil {
		return nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	switch locked.Source.Type {
	case "local":
		if locked.OverlayDigest == "" {
			current, stateErr := state.Current()
			if stateErr != nil {
				return stateErr
			}
			if current != nil {
				for _, entry := range current.Entries {
					if entry.ContentDigest != locked.ContentDigest || entry.Artifact == "" {
						continue
					}
					artifact, artifactErr := state.ArtifactPath(current.ID, entry.Artifact)
					if artifactErr != nil {
						return artifactErr
					}
					if copyErr := filetree.Copy(artifact, destination); copyErr != nil {
						return copyErr
					}
					if storeErr := store.Put(destination, locked.ContentDigest); storeErr != nil {
						return fmt.Errorf("store generation Skill: %w", storeErr)
					}
					return nil
				}
			}
		}
		sourcePath, err := document.Resolve(locked.Source.Path)
		if err != nil {
			return err
		}
		if err := filetree.Copy(sourcePath, destination); err != nil {
			return err
		}
	case "git":
		result, err := gitresolver.New().MaterializeSkill(ctx, gitresolver.Request{
			Repository: locked.Source.Repository,
			Revision:   locked.Source.ResolvedCommit,
			Path:       locked.Source.Path,
		}, destination)
		if err != nil {
			return err
		}
		if result.ResolvedCommit != locked.Source.ResolvedCommit || result.ContentDigest != locked.ContentDigest {
			return errors.New("materialized Git Skill does not match lockfile")
		}
	default:
		return fmt.Errorf("unsupported locked source type %q", locked.Source.Type)
	}
	digest, err := contenthash.Directory(destination)
	if err != nil {
		return err
	}
	if digest != locked.ContentDigest {
		return errors.New("materialized Skill does not match lockfile content")
	}
	if err := store.Put(destination, digest); err != nil {
		return fmt.Errorf("store locked Skill: %w", err)
	}
	return nil
}

func ensureLockedSourceStored(ctx context.Context, document catalog.Document, locked lockfile.LockedSkill) error {
	if err := store.Verify(locked.ContentDigest); err == nil {
		return nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	temporary, err := os.MkdirTemp("", "agx-store-legacy-source-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	return materializeLockedSource(ctx, document, locked, filepath.Join(temporary, "content"))
}

func storeCurrentOverlay(document catalog.Document, skill catalog.Skill, digest string) error {
	if digest == "" {
		return nil
	}
	overlayPath, err := document.Resolve(skill.Overlay)
	if err != nil {
		return err
	}
	if err := store.Put(overlayPath, digest); err != nil {
		return fmt.Errorf("store Overlay: %w", err)
	}
	return nil
}

func applyLockedOverlay(document catalog.Document, skill catalog.Skill, locked lockfile.LockedSkill, destination string) error {
	if locked.OverlayDigest == "" {
		return nil
	}
	temporary, err := os.MkdirTemp("", "agx-locked-overlay-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	overlayPath := filepath.Join(temporary, "content")
	if err := store.Materialize(locked.OverlayDigest, overlayPath); err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		currentPath, resolveErr := document.Resolve(skill.Overlay)
		if resolveErr != nil {
			return resolveErr
		}
		if err := overlay.Validate(currentPath); err != nil {
			return err
		}
		if err := filetree.Copy(currentPath, overlayPath); err != nil {
			return err
		}
		digest, err := contenthash.Directory(overlayPath)
		if err != nil {
			return err
		}
		if digest != locked.OverlayDigest {
			return errors.New("materialized Overlay does not match lockfile content")
		}
		if err := store.Put(overlayPath, digest); err != nil {
			return fmt.Errorf("store locked Overlay: %w", err)
		}
	}
	return overlay.Apply(destination, overlayPath)
}
