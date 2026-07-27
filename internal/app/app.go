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
	managedinstructions "github.com/paragon-h/agx/internal/instructions"
	"github.com/paragon-h/agx/internal/lockfile"
	"github.com/paragon-h/agx/internal/overlay"
	"github.com/paragon-h/agx/internal/store"
)

const (
	ExitSuccess          = 0
	ExitFailure          = 1
	ExitInvalidConfig    = 2
	ExitLockOutdated     = 3
	ExitPolicyDenied     = 4
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
	case "init":
		return r.init(args[1:])
	case "catalog":
		return r.catalogRegistry(args[1:])
	case "store":
		return r.storeCommand(args[1:])
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
	case "repair":
		return r.repair(args[1:])
	case "diff":
		return r.diff(ctx, args[1:])
	case "audit":
		return r.audit(ctx, args[1:])
	case "approve":
		return r.approve(ctx, args[1:])
	case "update":
		return r.update(ctx, args[1:])
	default:
		fmt.Fprintf(r.stderr, "AGX_UNKNOWN_COMMAND: unknown command %q\n", args[0])
		fmt.Fprintln(r.stderr, "Run 'agx help' to see available commands.")
		return ExitInvalidConfig
	}
}

func (r *Runner) list(args []string) int {
	if helpRequested(args) {
		fmt.Fprintln(r.stdout, "Usage: agx list [--catalog PATH | --catalogs NAME,...] [--profile NAME]")
		return ExitSuccess
	}
	flags := flag.NewFlagSet("list", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	catalogPath := flags.String("catalog", "", "catalog path (defaults to ./agx.yaml or the active Catalog)")
	catalogNames := flags.String("catalogs", "", "comma-separated registered Catalog names to compose")
	profileName := flags.String("profile", "", "select Skills and targets from a named profile")
	if err := flags.Parse(args); err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", err)
	}
	if flags.NArg() != 0 {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " ")))
	}
	if *catalogPath != "" && *catalogNames != "" {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", fmt.Errorf("--catalog and --catalogs cannot be used together"))
	}
	collection, err := loadCatalogCollection(*catalogPath, *catalogNames)
	if err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_CATALOG_INVALID", err)
	}
	selection, err := collection.SelectProfile(*profileName)
	if err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_PROFILE_INVALID", err)
	}
	for _, resource := range selection.Resources {
		targets := enabledTargets(resource.Skill.Targets)
		fmt.Fprintf(r.stdout, "%s\t%s\t%s\n", resource.QualifiedName, resource.Skill.Source.Type, strings.Join(targets, ","))
	}
	for _, resource := range selection.Instructions {
		_, _ = fmt.Fprintf(r.stdout, "instructions:%s\tinstructions\t%s\n", resource.QualifiedName, strings.Join(enabledTargets(resource.Instruction.Targets), ","))
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
	catalogPath := flags.String("catalog", "", "catalog path (defaults to ./agx.yaml or the active Catalog)")
	outputPath := flags.String("output", "", "lockfile path (defaults beside the catalog)")
	frozen := flags.Bool("frozen", false, "verify without writing or resolving remote sources")
	if err := flags.Parse(args); err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", err)
	}
	if flags.NArg() != 0 {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " ")))
	}
	resolvedCatalogPath, err := resolveCatalogPath(*catalogPath)
	if err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_CATALOG_NOT_FOUND", err)
	}
	document, err := catalog.Load(resolvedCatalogPath)
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
	if err := saveLockStoreReference(*outputPath, value); err != nil {
		return r.commandError(ExitFailure, "AGX_STORE_REFERENCE_WRITE_FAILED", err)
	}
	_, _ = fmt.Fprintf(r.stdout, "locked %d resource(s) -> %s\n", count, *outputPath)
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
	if len(value.Instructions) != len(document.Catalog.Instructions) {
		return r.commandError(ExitLockOutdated, "LOCK_OUTDATED", errors.New("catalog and lockfile contain different Instructions sets"))
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
		} else {
			if locked.Source.Type != "local" || locked.Source.Path != skill.Source.Path {
				return r.commandError(ExitLockOutdated, "LOCK_OUTDATED", fmt.Errorf("local source for %q differs from lockfile", name))
			}
			sourcePath, err := document.Resolve(skill.Source.Path)
			if err != nil {
				return r.commandError(ExitFailure, "AGX_SOURCE_READ_FAILED", err)
			}
			contentDigest, err := skillDigest(sourcePath)
			if err != nil {
				return r.commandError(ExitFailure, "AGX_SOURCE_READ_FAILED", err)
			}
			if contentDigest != locked.ContentDigest {
				return r.commandError(ExitLockOutdated, "LOCK_OUTDATED", fmt.Errorf("local source for %q changed", name))
			}
		}
		if err := store.Verify(locked.ContentDigest); err != nil {
			return r.commandError(ExitLockOutdated, "LOCK_OUTDATED", fmt.Errorf("Store object for %q is unavailable or invalid: %w", name, err))
		}
		overlayDigest := ""
		if skill.Overlay != "" {
			overlayPath, resolveErr := document.Resolve(skill.Overlay)
			if resolveErr != nil {
				return r.commandError(ExitFailure, "AGX_SOURCE_READ_FAILED", resolveErr)
			}
			if err := overlay.Validate(overlayPath); err != nil {
				return r.commandError(ExitFailure, "AGX_SOURCE_READ_FAILED", fmt.Errorf("overlay for %q: %w", name, err))
			}
			overlayDigest, err = contenthash.Directory(overlayPath)
			if err != nil {
				return r.commandError(ExitFailure, "AGX_SOURCE_READ_FAILED", err)
			}
		}
		if overlayDigest != locked.OverlayDigest {
			return r.commandError(ExitLockOutdated, "LOCK_OUTDATED", fmt.Errorf("overlay for %q changed", name))
		}
		if locked.OverlayDigest != "" {
			if err := store.Verify(locked.OverlayDigest); err != nil {
				return r.commandError(ExitLockOutdated, "LOCK_OUTDATED", fmt.Errorf("Overlay Store object for %q is unavailable or invalid: %w", name, err))
			}
		}
	}
	for name, declaration := range document.Catalog.Instructions {
		locked, ok := value.Instructions[name]
		if !ok {
			return r.commandError(ExitLockOutdated, "LOCK_OUTDATED", fmt.Errorf("instructions %q is missing from lockfile", name))
		}
		current, err := lockInstruction(document, declaration, time.Time{})
		if err != nil {
			return r.commandError(ExitFailure, "AGX_SOURCE_READ_FAILED", fmt.Errorf("instructions %q: %w", name, err))
		}
		if !sameLockedInstructionContent(current, locked) {
			return r.commandError(ExitLockOutdated, "LOCK_OUTDATED", fmt.Errorf("instructions %q changed", name))
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
		Instructions:  make(map[string]lockfile.LockedInstruction, len(document.Catalog.Instructions)),
	}
	for name, skill := range document.Catalog.Skills {
		locked := lockfile.LockedSkill{
			ContentDigest: "",
			LockedAt:      lockedAt.Format(time.RFC3339),
		}
		locked.Source, locked.ContentDigest, err = storeCurrentSkillSource(ctx, document, skill)
		if err != nil {
			return lockfile.Lockfile{}, 0, fmt.Errorf("skill %q: %w", name, err)
		}
		if skill.Overlay != "" {
			overlayPath, resolveErr := document.Resolve(skill.Overlay)
			if resolveErr != nil {
				return lockfile.Lockfile{}, 0, fmt.Errorf("skill %q overlay: %w", name, resolveErr)
			}
			if err := overlay.Validate(overlayPath); err != nil {
				return lockfile.Lockfile{}, 0, fmt.Errorf("skill %q overlay: %w", name, err)
			}
			locked.OverlayDigest, err = contenthash.Directory(overlayPath)
			if err != nil {
				return lockfile.Lockfile{}, 0, fmt.Errorf("skill %q overlay: %w", name, err)
			}
			if err := storeCurrentOverlay(document, skill, locked.OverlayDigest); err != nil {
				return lockfile.Lockfile{}, 0, fmt.Errorf("skill %q overlay: %w", name, err)
			}
			if err := validateOverlayApplication(ctx, document, skill, locked, overlayPath); err != nil {
				return lockfile.Lockfile{}, 0, fmt.Errorf("skill %q overlay cannot be applied: %w", name, err)
			}
		}
		if previous != nil {
			if old, ok := previous.Skills[name]; ok && old.Source == locked.Source && old.ContentDigest == locked.ContentDigest && old.OverlayDigest == locked.OverlayDigest {
				locked.LockedAt = old.LockedAt
			}
		}
		value.Skills[name] = locked
	}
	for name, declaration := range document.Catalog.Instructions {
		locked, lockErr := lockInstruction(document, declaration, lockedAt)
		if lockErr != nil {
			return lockfile.Lockfile{}, 0, fmt.Errorf("instructions %q: %w", name, lockErr)
		}
		if previous != nil {
			if old, ok := previous.Instructions[name]; ok && sameLockedInstructionContent(locked, old) {
				locked.LockedAt = old.LockedAt
			}
		}
		value.Instructions[name] = locked
	}
	return value, len(value.Skills) + len(value.Instructions), nil
}

func lockInstruction(document catalog.Document, declaration catalog.Instruction, lockedAt time.Time) (lockfile.LockedInstruction, error) {
	locked := lockfile.LockedInstruction{LockedAt: lockedAt.Format(time.RFC3339)}
	fragments := make([][]byte, 0, len(declaration.Sources))
	for _, source := range declaration.Sources {
		path, err := document.Resolve(source)
		if err != nil {
			return lockfile.LockedInstruction{}, err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return lockfile.LockedInstruction{}, err
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return lockfile.LockedInstruction{}, fmt.Errorf("source %q is not a regular file", source)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return lockfile.LockedInstruction{}, err
		}
		fragments = append(fragments, content)
		locked.Sources = append(locked.Sources, lockfile.LockedInstructionSource{Path: source, ContentDigest: contenthash.Bytes(content)})
	}
	content, err := managedinstructions.Compose(fragments)
	if err != nil {
		return lockfile.LockedInstruction{}, err
	}
	locked.Content = string(content)
	locked.ContentDigest = contenthash.Bytes(content)
	return locked, nil
}

func sameLockedInstructionContent(left, right lockfile.LockedInstruction) bool {
	if left.Content != right.Content || left.ContentDigest != right.ContentDigest || len(left.Sources) != len(right.Sources) {
		return false
	}
	for index := range left.Sources {
		if left.Sources[index] != right.Sources[index] {
			return false
		}
	}
	return true
}

func instructionDeclarationMatchesLock(declaration catalog.Instruction, locked lockfile.LockedInstruction) bool {
	if len(declaration.Sources) != len(locked.Sources) {
		return false
	}
	for index, source := range declaration.Sources {
		if locked.Sources[index].Path != source {
			return false
		}
	}
	return true
}

func validateOverlayApplication(ctx context.Context, document catalog.Document, skill catalog.Skill, locked lockfile.LockedSkill, overlayPath string) error {
	temporary, err := os.MkdirTemp("", "agx-overlay-lock-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	destination := filepath.Join(temporary, "content")
	if err := materializeLockedSource(ctx, document, locked, destination); err != nil {
		return err
	}
	return overlay.Apply(destination, overlayPath)
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
  init      Create a new local catalog
  catalog   Register and select local catalogs
  store     Inspect and clean the content-addressed Store
  list      List skills in the active catalog
  lock      Resolve sources and write or verify the lockfile
  plan      Preview changes to agent global directories
  apply     Apply a previously reviewed plan
  status    Show the active installation generation
  rollback  Restore a previous installation generation
  repair    Recover an interrupted installation transaction
  diff      Compare locked and candidate Skill content
  audit     Scan locked or candidate Skill content for risks
  approve   Approve the currently locked Skill content
  update    Check or accept newer Skill source content
  doctor    Check configuration and agent integration
  version   Print the AGX version
  help      Show this help

Project status: Skill management, Codex/Pi/OpenCode global Instructions, Profiles, local multi-Catalog composition, and the Codex, Claude, Pi, and OpenCode adapters are implemented; later milestone features remain under development.`)
}
