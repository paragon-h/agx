package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/paragon-h/agx/internal/catalog"
	"github.com/paragon-h/agx/internal/contenthash"
	"github.com/paragon-h/agx/internal/filetree"
	"github.com/paragon-h/agx/internal/lockfile"
	"github.com/paragon-h/agx/internal/overlay"
	gitresolver "github.com/paragon-h/agx/internal/resolver/git"
	"github.com/paragon-h/agx/internal/state"
)

type reviewInput struct {
	document      catalog.Document
	qualifiedName string
	skill         catalog.Skill
	lockedSkill   lockfile.LockedSkill
}

type reviewVersion struct {
	Root          string
	Digest        string
	SourceDigest  string
	OverlayDigest string
	Commit        string
	cleanup       func()
}

func loadReviewInput(catalogPath, lockPath, skillName string) (reviewInput, error) {
	var err error
	catalogPath, err = resolveCatalogPath(catalogPath)
	if err != nil {
		return reviewInput{}, err
	}
	document, err := catalog.Load(catalogPath)
	if err != nil {
		return reviewInput{}, err
	}
	if lockPath == "" {
		lockPath = filepath.Join(document.Root, "agx.lock")
	}
	locked, err := lockfile.Load(lockPath)
	if err != nil {
		return reviewInput{}, err
	}
	catalogDigest, err := contenthash.File(document.Path)
	if err != nil {
		return reviewInput{}, err
	}
	if catalogDigest != locked.CatalogDigest {
		return reviewInput{}, fmt.Errorf("catalog content differs from lockfile; run agx lock before review")
	}
	shortName := skillName
	if strings.Contains(skillName, "/") {
		prefix := document.Catalog.Metadata.Name + "/"
		if !strings.HasPrefix(skillName, prefix) {
			return reviewInput{}, fmt.Errorf("skill %q does not belong to catalog %q", skillName, document.Catalog.Metadata.Name)
		}
		shortName = strings.TrimPrefix(skillName, prefix)
	}
	skill, ok := document.Catalog.Skills[shortName]
	if !ok {
		return reviewInput{}, fmt.Errorf("skill %q is not declared", skillName)
	}
	lockedSkill, ok := locked.Skills[shortName]
	if !ok {
		return reviewInput{}, fmt.Errorf("skill %q is missing from lockfile", skillName)
	}
	overlayDigest := ""
	if skill.Overlay != "" {
		overlayPath, err := document.Resolve(skill.Overlay)
		if err != nil {
			return reviewInput{}, fmt.Errorf("resolve overlay for %q: %w", skillName, err)
		}
		if err := overlay.Validate(overlayPath); err != nil {
			return reviewInput{}, fmt.Errorf("validate overlay for %q: %w", skillName, err)
		}
		overlayDigest, err = contenthash.Directory(overlayPath)
		if err != nil {
			return reviewInput{}, fmt.Errorf("read overlay for %q: %w", skillName, err)
		}
	}
	if overlayDigest != lockedSkill.OverlayDigest {
		return reviewInput{}, fmt.Errorf("overlay for %q differs from lockfile; run agx lock before review", skillName)
	}
	return reviewInput{
		document:      document,
		qualifiedName: catalog.QualifiedName(document.Catalog.Metadata.Name, shortName),
		skill:         skill,
		lockedSkill:   lockedSkill,
	}, nil
}

func materializeReviewVersion(ctx context.Context, input reviewInput, candidate bool) (reviewVersion, error) {
	temporary, err := os.MkdirTemp("", "agx-review-*")
	if err != nil {
		return reviewVersion{}, err
	}
	cleanup := func() { _ = os.RemoveAll(temporary) }
	destination := filepath.Join(temporary, "content")
	version := reviewVersion{Root: destination, cleanup: cleanup}
	if !candidate {
		switch input.lockedSkill.Source.Type {
		case "local":
			err = materializeLockedLocal(input, destination)
		case "git":
			var result gitresolver.Result
			result, err = gitresolver.New().MaterializeSkill(ctx, gitresolver.Request{
				Repository: input.lockedSkill.Source.Repository,
				Revision:   input.lockedSkill.Source.ResolvedCommit,
				Path:       input.lockedSkill.Source.Path,
			}, destination)
			version.Commit = result.ResolvedCommit
		default:
			err = fmt.Errorf("unsupported locked source type %q", input.lockedSkill.Source.Type)
		}
		if err != nil {
			cleanup()
			return reviewVersion{}, err
		}
		version.SourceDigest, err = contenthash.Directory(destination)
		if err == nil && version.SourceDigest != input.lockedSkill.ContentDigest {
			err = fmt.Errorf("locked content for %s is unavailable or changed", input.qualifiedName)
		}
	} else {
		switch input.skill.Source.Type {
		case "local":
			var sourcePath string
			sourcePath, err = input.document.Resolve(input.skill.Source.Path)
			if err == nil {
				err = filetree.Copy(sourcePath, destination)
			}
		case "git":
			var result gitresolver.Result
			result, err = gitresolver.New().MaterializeSkill(ctx, gitresolver.Request{
				Repository: input.skill.Source.Repository,
				Revision:   input.skill.Source.Revision,
				Path:       input.skill.Source.Path,
			}, destination)
			version.Commit = result.ResolvedCommit
			version.SourceDigest = result.ContentDigest
		default:
			err = fmt.Errorf("unsupported source type %q", input.skill.Source.Type)
		}
		if err == nil && version.SourceDigest == "" {
			version.SourceDigest, err = contenthash.Directory(destination)
		}
	}
	if err != nil {
		cleanup()
		return reviewVersion{}, err
	}
	if input.skill.Overlay != "" {
		overlayPath, resolveErr := input.document.Resolve(input.skill.Overlay)
		if resolveErr != nil {
			cleanup()
			return reviewVersion{}, resolveErr
		}
		version.OverlayDigest, err = contenthash.Directory(overlayPath)
		if err == nil {
			err = overlay.Apply(destination, overlayPath)
		}
		if err != nil {
			cleanup()
			return reviewVersion{}, fmt.Errorf("apply overlay for %s: %w", input.qualifiedName, err)
		}
	}
	version.Digest, err = contenthash.Directory(destination)
	if err != nil {
		cleanup()
		return reviewVersion{}, err
	}
	return version, nil
}

func materializeLockedLocal(input reviewInput, destination string) error {
	current, err := state.Current()
	if err != nil {
		return err
	}
	if current != nil && input.skill.Overlay == "" {
		for _, entry := range current.Entries {
			if entry.Skill != input.qualifiedName || entry.ContentDigest != input.lockedSkill.ContentDigest || entry.Artifact == "" {
				continue
			}
			artifact, err := state.ArtifactPath(current.ID, entry.Artifact)
			if err != nil {
				return err
			}
			return filetree.Copy(artifact, destination)
		}
	}
	sourcePath, err := input.document.Resolve(input.lockedSkill.Source.Path)
	if err != nil {
		return err
	}
	return filetree.Copy(sourcePath, destination)
}

func (v reviewVersion) Close() {
	if v.cleanup != nil {
		v.cleanup()
	}
}

func normalizeReviewArgs(args []string, booleanFlags map[string]bool) ([]string, error) {
	flagArguments := make([]string, 0, len(args))
	positionals := make([]string, 0, 1)
	for i := 0; i < len(args); i++ {
		argument := args[i]
		if !strings.HasPrefix(argument, "-") || argument == "-" {
			positionals = append(positionals, argument)
			continue
		}
		flagArguments = append(flagArguments, argument)
		name := strings.TrimLeft(argument, "-")
		if separator := strings.IndexByte(name, '='); separator >= 0 {
			continue
		}
		if booleanFlags[name] {
			continue
		}
		if i+1 >= len(args) {
			return nil, fmt.Errorf("flag %s requires a value", argument)
		}
		i++
		flagArguments = append(flagArguments, args[i])
	}
	return append(flagArguments, positionals...), nil
}
