package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/paragon-h/agx/internal/catalog"
	"github.com/paragon-h/agx/internal/contenthash"
	"github.com/paragon-h/agx/internal/lockfile"
	gitresolver "github.com/paragon-h/agx/internal/resolver/git"
)

const (
	ExitSuccess          = 0
	ExitFailure          = 1
	ExitInvalidConfig    = 2
	ExitLockOutdated     = 3
	ExitSourceFailure    = 6
	ExitAgentUnavailable = 7
)

type Runner struct {
	stdout  io.Writer
	stderr  io.Writer
	version string
}

func New(stdout, stderr io.Writer, version string) *Runner {
	return &Runner{stdout: stdout, stderr: stderr, version: version}
}

func (r *Runner) Run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		r.writeHelp(r.stdout)
		return ExitSuccess
	}

	switch args[0] {
	case "help", "-h", "--help":
		r.writeHelp(r.stdout)
		return ExitSuccess
	case "version", "--version":
		fmt.Fprintf(r.stdout, "agx %s\n", r.version)
		return ExitSuccess
	case "list":
		return r.list(args[1:])
	case "lock":
		return r.lock(ctx, args[1:])
	case "doctor":
		return r.doctor(ctx, args[1:])
	case "plan":
		return r.plan(ctx, args[1:])
	case "apply":
		return r.apply(ctx, args[1:])
	case "status":
		return r.status(args[1:])
	case "rollback":
		return r.rollback(args[1:])
	default:
		fmt.Fprintf(r.stderr, "AGX_UNKNOWN_COMMAND: unknown command %q\n", args[0])
		fmt.Fprintln(r.stderr, "Run 'agx help' to see available commands.")
		return ExitInvalidConfig
	}
}

func (r *Runner) list(args []string) int {
	if helpRequested(args) {
		fmt.Fprintln(r.stdout, "Usage: agx list [--catalog PATH]")
		return ExitSuccess
	}
	flags := flag.NewFlagSet("list", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	catalogPath := flags.String("catalog", "agx.yaml", "catalog path")
	if err := flags.Parse(args); err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", err)
	}
	if flags.NArg() != 0 {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " ")))
	}
	document, err := catalog.Load(*catalogPath)
	if err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_CATALOG_INVALID", err)
	}
	for _, name := range sortedSkillNames(document.Catalog) {
		skill := document.Catalog.Skills[name]
		targets := enabledTargets(skill.Targets)
		fmt.Fprintf(r.stdout, "%s\t%s\t%s\n", catalog.QualifiedName(document.Catalog.Metadata.Name, name), skill.Source.Type, strings.Join(targets, ","))
	}
	return ExitSuccess
}

func (r *Runner) lock(ctx context.Context, args []string) int {
	if helpRequested(args) {
		fmt.Fprintln(r.stdout, "Usage: agx lock [--catalog PATH] [--output PATH] [--frozen]")
		return ExitSuccess
	}
	flags := flag.NewFlagSet("lock", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	catalogPath := flags.String("catalog", "agx.yaml", "catalog path")
	outputPath := flags.String("output", "", "lockfile path (defaults beside the catalog)")
	frozen := flags.Bool("frozen", false, "verify without writing or resolving remote sources")
	if err := flags.Parse(args); err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", err)
	}
	if flags.NArg() != 0 {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " ")))
	}
	document, err := catalog.Load(*catalogPath)
	if err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_CATALOG_INVALID", err)
	}
	if *outputPath == "" {
		*outputPath = filepath.Join(document.Root, "agx.lock")
	}
	if *frozen {
		return r.verifyFrozen(document, *outputPath)
	}
	var previous *lockfile.Lockfile
	if existing, loadErr := lockfile.Load(*outputPath); loadErr == nil {
		previous = &existing
	}
	value, count, err := buildLock(ctx, document, time.Now().UTC(), previous)
	if err != nil {
		return r.commandError(ExitSourceFailure, "AGX_SOURCE_RESOLUTION_FAILED", err)
	}
	if err := lockfile.Write(*outputPath, value); err != nil {
		return r.commandError(ExitFailure, "AGX_LOCK_WRITE_FAILED", err)
	}
	fmt.Fprintf(r.stdout, "locked %d skill(s) -> %s\n", count, *outputPath)
	return ExitSuccess
}

func (r *Runner) verifyFrozen(document catalog.Document, path string) int {
	value, err := lockfile.Load(path)
	if err != nil {
		return r.commandError(ExitLockOutdated, "AGX_LOCK_INVALID", err)
	}
	catalogDigest, err := contenthash.File(document.Path)
	if err != nil {
		return r.commandError(ExitFailure, "AGX_CATALOG_READ_FAILED", err)
	}
	if value.CatalogDigest != catalogDigest {
		return r.commandError(ExitLockOutdated, "LOCK_OUTDATED", errors.New("catalog content differs from lockfile"))
	}
	if len(value.Skills) != len(document.Catalog.Skills) {
		return r.commandError(ExitLockOutdated, "LOCK_OUTDATED", errors.New("catalog and lockfile contain different skill sets"))
	}
	for name, skill := range document.Catalog.Skills {
		locked, ok := value.Skills[name]
		if !ok {
			return r.commandError(ExitLockOutdated, "LOCK_OUTDATED", fmt.Errorf("skill %q is missing from lockfile", name))
		}
		if skill.Source.Type == "git" {
			if locked.Source.Type != "git" || locked.Source.Repository != skill.Source.Repository || locked.Source.RequestedRevision != skill.Source.Revision || locked.Source.Path != skill.Source.Path {
				return r.commandError(ExitLockOutdated, "LOCK_OUTDATED", fmt.Errorf("git source for %q differs from lockfile", name))
			}
			continue
		}
		if locked.Source.Type != "local" || locked.Source.Path != skill.Source.Path {
			return r.commandError(ExitLockOutdated, "LOCK_OUTDATED", fmt.Errorf("local source for %q differs from lockfile", name))
		}
		contentDigest, err := skillDigest(document.Resolve(skill.Source.Path))
		if err != nil {
			return r.commandError(ExitFailure, "AGX_SOURCE_READ_FAILED", err)
		}
		if contentDigest != locked.ContentDigest {
			return r.commandError(ExitLockOutdated, "LOCK_OUTDATED", fmt.Errorf("local source for %q changed", name))
		}
		overlayDigest := ""
		if skill.Overlay != "" {
			overlayDigest, err = contenthash.Directory(document.Resolve(skill.Overlay))
			if err != nil {
				return r.commandError(ExitFailure, "AGX_SOURCE_READ_FAILED", err)
			}
		}
		if overlayDigest != locked.OverlayDigest {
			return r.commandError(ExitLockOutdated, "LOCK_OUTDATED", fmt.Errorf("overlay for %q changed", name))
		}
	}
	fmt.Fprintf(r.stdout, "lockfile verified (frozen): %s\n", path)
	return ExitSuccess
}

func buildLock(ctx context.Context, document catalog.Document, lockedAt time.Time, previous *lockfile.Lockfile) (lockfile.Lockfile, int, error) {
	catalogDigest, err := contenthash.File(document.Path)
	if err != nil {
		return lockfile.Lockfile{}, 0, err
	}
	value := lockfile.Lockfile{
		APIVersion:    lockfile.APIVersion,
		Kind:          lockfile.Kind,
		CatalogDigest: catalogDigest,
		Skills:        make(map[string]lockfile.LockedSkill, len(document.Catalog.Skills)),
	}
	for name, skill := range document.Catalog.Skills {
		locked := lockfile.LockedSkill{
			ContentDigest: "",
			LockedAt:      lockedAt.Format(time.RFC3339),
		}
		switch skill.Source.Type {
		case "local":
			locked.Source = lockfile.LockedSource{Type: "local", Path: skill.Source.Path}
			locked.ContentDigest, err = skillDigest(document.Resolve(skill.Source.Path))
		case "git":
			resolved, resolveErr := gitresolver.New().ResolveSkill(ctx, gitresolver.Request{
				Repository: skill.Source.Repository,
				Revision:   skill.Source.Revision,
				Path:       skill.Source.Path,
			})
			if resolveErr != nil {
				return lockfile.Lockfile{}, 0, fmt.Errorf("skill %q: %w", name, resolveErr)
			}
			locked.Source = lockfile.LockedSource{
				Type:              "git",
				Repository:        skill.Source.Repository,
				RequestedRevision: skill.Source.Revision,
				ResolvedCommit:    resolved.ResolvedCommit,
				Path:              skill.Source.Path,
			}
			locked.ContentDigest = resolved.ContentDigest
		default:
			return lockfile.Lockfile{}, 0, fmt.Errorf("unsupported source type %q", skill.Source.Type)
		}
		if err != nil {
			return lockfile.Lockfile{}, 0, fmt.Errorf("skill %q: %w", name, err)
		}
		if skill.Overlay != "" {
			locked.OverlayDigest, err = contenthash.Directory(document.Resolve(skill.Overlay))
			if err != nil {
				return lockfile.Lockfile{}, 0, fmt.Errorf("skill %q overlay: %w", name, err)
			}
		}
		if previous != nil {
			if old, ok := previous.Skills[name]; ok && old.Source == locked.Source && old.ContentDigest == locked.ContentDigest && old.OverlayDigest == locked.OverlayDigest {
				locked.LockedAt = old.LockedAt
			}
		}
		value.Skills[name] = locked
	}
	return value, len(value.Skills), nil
}

func helpRequested(args []string) bool {
	return len(args) == 1 && (args[0] == "-h" || args[0] == "--help")
}

func skillDigest(root string) (string, error) {
	manifestPath := filepath.Join(root, "SKILL.md")
	info, err := os.Lstat(manifestPath)
	if err != nil {
		return "", fmt.Errorf("required SKILL.md: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("required SKILL.md must be a regular file")
	}
	return contenthash.Directory(root)
}

func enabledTargets(targets map[string]catalog.TargetConfig) []string {
	result := make([]string, 0, len(targets))
	for target, config := range targets {
		if config.Enabled != nil && !*config.Enabled {
			continue
		}
		result = append(result, target)
	}
	sort.Strings(result)
	return result
}

func (r *Runner) commandError(exit int, code string, err error) int {
	fmt.Fprintf(r.stderr, "%s: %v\n", code, err)
	return exit
}

func (r *Runner) writeHelp(w io.Writer) {
	fmt.Fprintln(w, `AGX manages global extensions for AI coding agents.

Usage:
  agx <command>

Commands:
  list      List skills in the active catalog
  lock      Resolve sources and write or verify the lockfile
  plan      Preview changes to agent global directories
  apply     Apply a previously reviewed plan
  status    Show the active installation generation
  rollback  Restore a previous installation generation
  doctor    Check configuration and agent integration
  version   Print the AGX version
  help      Show this help

Project status: early implementation; Milestone 1 commands may be incomplete.`)
}
