package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/paragon-h/agx/internal/catalog"
	"github.com/paragon-h/agx/internal/contenthash"
	"github.com/paragon-h/agx/internal/lockfile"
	"github.com/paragon-h/agx/internal/overlay"
	"github.com/paragon-h/agx/internal/state"
	"github.com/paragon-h/agx/internal/store"
)

const ExitTargetConflict = 5

type planReport struct {
	Catalog  string        `json:"catalog,omitempty"`
	Lockfile string        `json:"lockfile,omitempty"`
	Catalogs []planCatalog `json:"catalogs,omitempty"`
	Profile  string        `json:"profile,omitempty"`
	Changes  []planChange  `json:"changes"`
	Summary  planSummary   `json:"summary"`
}

type planCatalog struct {
	Name     string `json:"name"`
	Catalog  string `json:"catalog"`
	Lockfile string `json:"lockfile"`
}

type planChange struct {
	Target        string `json:"target"`
	Skill         string `json:"skill"`
	Path          string `json:"path"`
	Action        string `json:"action"`
	DesiredDigest string `json:"desiredDigest,omitempty"`
	CurrentDigest string `json:"currentDigest,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

type planSummary struct {
	Add       int `json:"add"`
	Adopt     int `json:"adopt"`
	Update    int `json:"update"`
	Unchanged int `json:"unchanged"`
	Remove    int `json:"remove"`
	Conflict  int `json:"conflict"`
}

func (r *Runner) plan(ctx context.Context, args []string) int {
	if helpRequested(args) {
		fmt.Fprintln(r.stdout, "Usage: agx plan [--catalog PATH | --catalogs NAME,...] [--lockfile PATH] [--profile NAME] [--adopt] [--allow-empty] [--json]")
		return ExitSuccess
	}
	flags := flag.NewFlagSet("plan", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	catalogPath := flags.String("catalog", "", "catalog path (defaults to ./agx.yaml or the active Catalog)")
	catalogNames := flags.String("catalogs", "", "comma-separated registered Catalog names to compose")
	lockPath := flags.String("lockfile", "", "lockfile path (defaults beside the catalog)")
	profileName := flags.String("profile", "", "select Skills and targets from a named profile")
	adopt := flags.Bool("adopt", false, "allow adoption of unmanaged targets with identical content")
	allowEmpty := flags.Bool("allow-empty", false, "allow an empty desired selection to remove managed Skills")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", err)
	}
	if flags.NArg() != 0 {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " ")))
	}
	if *catalogPath != "" && *catalogNames != "" {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", fmt.Errorf("--catalog and --catalogs cannot be used together"))
	}
	if *catalogNames != "" && *lockPath != "" {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", fmt.Errorf("--lockfile cannot be used with --catalogs"))
	}
	collection, err := loadCatalogCollection(*catalogPath, *catalogNames)
	if err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_CATALOG_INVALID", err)
	}
	desired, code, err := loadDesiredState(ctx, collection, *profileName, *lockPath)
	if err != nil {
		return r.commandError(code, desiredStateErrorCode(code, err), err)
	}
	if err := requireApprovals(desired); err != nil {
		return r.commandError(ExitPolicyDenied, "AGX_APPROVAL_REQUIRED", err)
	}
	current, err := state.Current()
	if err != nil {
		return r.commandError(ExitFailure, "AGX_STATE_INVALID", err)
	}
	managed := make(map[string]state.Entry)
	if current != nil {
		managed = current.ManagedByPath()
	}
	if err := requireEmptySelectionConfirmation(desired, current, *allowEmpty); err != nil {
		return r.commandError(ExitPolicyDenied, "AGX_EMPTY_CATALOG", err)
	}
	report, code, err := buildPlan(ctx, desired, *adopt, managed)
	if err != nil {
		return r.commandError(code, planErrorCode(code), err)
	}
	if *jsonOutput {
		if err := json.NewEncoder(r.stdout).Encode(report); err != nil {
			return r.commandError(ExitFailure, "AGX_OUTPUT_FAILED", err)
		}
	} else {
		renderPlanText(r.stdout, report)
	}
	if report.Summary.Conflict > 0 {
		return ExitTargetConflict
	}
	return ExitSuccess
}

func requireEmptySelectionConfirmation(desired desiredState, current *state.Generation, allowEmpty bool) error {
	if len(desired.Skills) != 0 || current == nil || len(current.Entries) == 0 || allowEmpty {
		return nil
	}
	if desired.Profile != "" {
		return fmt.Errorf("profile %q selects no installable skills and would remove %d managed installation(s); rerun with --allow-empty to confirm", desired.Profile, len(current.Entries))
	}
	return fmt.Errorf("selected Catalogs contain no skills and would remove %d managed installation(s); rerun with --allow-empty to confirm", len(current.Entries))
}

func buildPlan(ctx context.Context, desired desiredState, adopt bool, managed map[string]state.Entry) (planReport, int, error) {
	report := newPlanReport(desired)
	targetPaths := make(map[string]string)
	for _, targetName := range desired.targetNames() {
		adapter, ok := adapterFor(targetName)
		if !ok {
			return planReport{}, ExitAgentUnavailable, fmt.Errorf("target %q has no built-in adapter", targetName)
		}
		detection, err := adapter.Detect(ctx)
		if err != nil {
			return planReport{}, ExitAgentUnavailable, err
		}
		if !detection.Installed {
			return planReport{}, ExitAgentUnavailable, fmt.Errorf("target %q executable is not available", targetName)
		}
		paths, err := adapter.ResolvePaths(ctx)
		if err != nil {
			return planReport{}, ExitAgentUnavailable, err
		}
		targetPaths[targetName] = paths.SkillsDir
	}
	for _, entry := range managed {
		adapter, ok := adapterFor(entry.Target)
		if !ok {
			return planReport{}, ExitTargetConflict, fmt.Errorf("managed target %q has no built-in adapter", entry.Target)
		}
		paths, err := adapter.ResolvePaths(ctx)
		if err != nil {
			return planReport{}, ExitTargetConflict, err
		}
		root := filepath.Clean(paths.SkillsDir)
		if activeRoot, ok := targetPaths[entry.Target]; ok && filepath.Clean(activeRoot) != root {
			return planReport{}, ExitTargetConflict, fmt.Errorf("managed target root changed for %q", entry.Target)
		}
		separator := strings.LastIndex(entry.Skill, "/")
		shortName := ""
		if separator >= 0 {
			shortName = entry.Skill[separator+1:]
		}
		if filepath.Dir(filepath.Clean(entry.Path)) != root || !catalog.ValidName(shortName) || filepath.Base(entry.Path) != shortName {
			return planReport{}, ExitTargetConflict, fmt.Errorf("managed path is outside the %s Skill root: %s", entry.Target, entry.Path)
		}
		targetPaths[entry.Target] = root
	}
	if err := validateTargetRoots(targetPaths); err != nil {
		return planReport{}, ExitTargetConflict, err
	}

	desiredOwners := make(map[string]string)
	for _, selectedSkill := range desired.Skills {
		rendered, err := materializeReviewVersion(ctx, reviewInput{
			document:      selectedSkill.Document,
			qualifiedName: selectedSkill.QualifiedName,
			skill:         selectedSkill.Skill,
			lockedSkill:   selectedSkill.LockedSkill,
		}, false)
		if err != nil {
			return planReport{}, ExitSourceFailure, fmt.Errorf("render skill %q: %w", selectedSkill.QualifiedName, err)
		}
		desiredDigest := rendered.Digest
		rendered.Close()
		for _, targetName := range enabledTargets(selectedSkill.Skill.Targets) {
			targetPath := filepath.Join(targetPaths[targetName], selectedSkill.Name)
			cleanedTargetPath := filepath.Clean(targetPath)
			if existing, exists := desiredOwners[cleanedTargetPath]; exists {
				return planReport{}, ExitTargetConflict, fmt.Errorf("skills %s and %s resolve to the same %s target path %s", existing, selectedSkill.QualifiedName, targetName, targetPath)
			}
			desiredOwners[cleanedTargetPath] = selectedSkill.QualifiedName
			managedEntry, isManaged := managed[filepath.Clean(targetPath)]
			change := inspectPlanTarget(targetName, selectedSkill.QualifiedName, targetPath, desiredDigest, adopt, isManaged, managedEntry)
			report.Changes = append(report.Changes, change)
			switch change.Action {
			case "add":
				report.Summary.Add++
			case "adopt":
				report.Summary.Adopt++
			case "update":
				report.Summary.Update++
			case "unchanged":
				report.Summary.Unchanged++
			case "conflict":
				report.Summary.Conflict++
			}
		}
	}
	desiredPaths := make(map[string]struct{}, len(report.Changes))
	for _, change := range report.Changes {
		desiredPaths[filepath.Clean(change.Path)] = struct{}{}
	}
	for path, entry := range managed {
		if _, desired := desiredPaths[path]; desired {
			continue
		}
		change := planChange{Target: entry.Target, Skill: entry.Skill, Path: path, Action: "remove", Reason: "managed target is no longer declared"}
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			change.Reason = "managed target is already absent"
		} else if err != nil {
			change.Action = "conflict"
			change.Reason = err.Error()
			report.Summary.Conflict++
		} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			change.Action = "conflict"
			change.Reason = "managed target is not a regular directory"
			report.Summary.Conflict++
		} else if digest, digestErr := contenthash.Directory(path); digestErr != nil {
			change.Action = "conflict"
			change.Reason = digestErr.Error()
			report.Summary.Conflict++
		} else if digest != entry.ContentDigest {
			change.Action = "conflict"
			change.Reason = "managed target was modified outside AGX"
			report.Summary.Conflict++
		} else {
			change.CurrentDigest = digest
		}
		report.Changes = append(report.Changes, change)
		if change.Action == "remove" {
			report.Summary.Remove++
		}
	}
	sort.Slice(report.Changes, func(i, j int) bool {
		if report.Changes[i].Target != report.Changes[j].Target {
			return report.Changes[i].Target < report.Changes[j].Target
		}
		return report.Changes[i].Skill < report.Changes[j].Skill
	})
	return report, ExitSuccess, nil
}

func newPlanReport(desired desiredState) planReport {
	report := planReport{Profile: desired.Profile}
	if len(desired.Catalogs) == 1 {
		report.Catalog = desired.Catalogs[0].Document.Path
		report.Lockfile = desired.Catalogs[0].LockPath
		return report
	}
	for _, input := range desired.Catalogs {
		report.Catalogs = append(report.Catalogs, planCatalog{Name: input.Name, Catalog: input.Document.Path, Lockfile: input.LockPath})
	}
	return report
}

func validateTargetRoots(targetPaths map[string]string) error {
	targets := make([]string, 0, len(targetPaths))
	for target := range targetPaths {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	for i, leftTarget := range targets {
		leftPath := filepath.Clean(targetPaths[leftTarget])
		for _, rightTarget := range targets[i+1:] {
			rightPath := filepath.Clean(targetPaths[rightTarget])
			if pathsOverlap(leftPath, rightPath) {
				return fmt.Errorf("target roots overlap: %s=%s, %s=%s", leftTarget, leftPath, rightTarget, rightPath)
			}
		}
	}
	return nil
}

func pathsOverlap(left, right string) bool {
	if left == right {
		return true
	}
	relative, err := filepath.Rel(left, right)
	if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return true
	}
	relative, err = filepath.Rel(right, left)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func verifyPlanSources(ctx context.Context, document catalog.Document, locked lockfile.Lockfile) (int, error) {
	catalogDigest, err := contenthash.File(document.Path)
	if err != nil {
		return ExitFailure, err
	}
	if catalogDigest != locked.CatalogDigest {
		return ExitLockOutdated, errors.New("catalog content differs from lockfile")
	}
	if len(document.Catalog.Skills) != len(locked.Skills) {
		return ExitLockOutdated, errors.New("catalog and lockfile contain different skill sets")
	}
	for _, name := range sortedSkillNames(document.Catalog) {
		skill := document.Catalog.Skills[name]
		lockedSkill, ok := locked.Skills[name]
		if !ok {
			return ExitLockOutdated, fmt.Errorf("skill %q is missing from lockfile", name)
		}
		switch skill.Source.Type {
		case "local":
			if lockedSkill.Source.Type != "local" || lockedSkill.Source.Path != skill.Source.Path {
				return ExitLockOutdated, fmt.Errorf("local source for %q differs from lockfile", name)
			}
			sourcePath, err := document.Resolve(skill.Source.Path)
			if err != nil {
				return ExitSourceFailure, fmt.Errorf("skill %q: %w", name, err)
			}
			if _, err := os.Lstat(sourcePath); os.IsNotExist(err) {
				if err := ensureLockedSourceStored(ctx, document, lockedSkill); err != nil {
					return ExitSourceFailure, fmt.Errorf("skill %q source is unavailable and its Store object cannot be used: %w", name, err)
				}
				break
			} else if err != nil {
				return ExitSourceFailure, fmt.Errorf("skill %q: %w", name, err)
			}
			digest, err := skillDigest(sourcePath)
			if err != nil {
				return ExitSourceFailure, fmt.Errorf("skill %q: %w", name, err)
			}
			if digest != lockedSkill.ContentDigest {
				return ExitLockOutdated, fmt.Errorf("local source for %q changed", name)
			}
			if err := store.Put(sourcePath, digest); err != nil {
				return ExitSourceFailure, fmt.Errorf("skill %q Store object: %w", name, err)
			}
		case "git":
			if lockedSkill.Source.Type != "git" || lockedSkill.Source.Repository != skill.Source.Repository || lockedSkill.Source.RequestedRevision != skill.Source.Revision || lockedSkill.Source.Path != skill.Source.Path {
				return ExitLockOutdated, fmt.Errorf("Git source for %q differs from lockfile", name)
			}
			if err := ensureLockedSourceStored(ctx, document, lockedSkill); err != nil {
				return ExitSourceFailure, fmt.Errorf("skill %q Store object: %w", name, err)
			}
		default:
			return ExitInvalidConfig, fmt.Errorf("unsupported source type %q", skill.Source.Type)
		}
		overlayDigest := ""
		if skill.Overlay != "" {
			overlayPath, resolveErr := document.Resolve(skill.Overlay)
			if resolveErr != nil {
				return ExitSourceFailure, fmt.Errorf("skill %q overlay: %w", name, resolveErr)
			}
			if _, err := os.Lstat(overlayPath); os.IsNotExist(err) {
				if lockedSkill.OverlayDigest == "" {
					return ExitLockOutdated, fmt.Errorf("overlay for %q differs from lockfile", name)
				}
				if err := store.Verify(lockedSkill.OverlayDigest); err != nil {
					return ExitSourceFailure, fmt.Errorf("skill %q Overlay is unavailable and its Store object cannot be used: %w", name, err)
				}
				overlayDigest = lockedSkill.OverlayDigest
			} else if err != nil {
				return ExitSourceFailure, fmt.Errorf("skill %q overlay: %w", name, err)
			} else {
				if err := overlay.Validate(overlayPath); err != nil {
					return ExitSourceFailure, fmt.Errorf("skill %q overlay: %w", name, err)
				}
				overlayDigest, err = contenthash.Directory(overlayPath)
				if err != nil {
					return ExitSourceFailure, fmt.Errorf("skill %q overlay: %w", name, err)
				}
				if overlayDigest == lockedSkill.OverlayDigest {
					if err := store.Put(overlayPath, overlayDigest); err != nil {
						return ExitSourceFailure, fmt.Errorf("skill %q Overlay Store object: %w", name, err)
					}
				}
			}
		}
		if overlayDigest != lockedSkill.OverlayDigest {
			return ExitLockOutdated, fmt.Errorf("overlay for %q changed", name)
		}
	}
	return ExitSuccess, nil
}

func sortedSkillNames(value catalog.Catalog) []string {
	names := make([]string, 0, len(value.Skills))
	for name := range value.Skills {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func inspectPlanTarget(target, skill, path, desiredDigest string, adopt, managed bool, managedEntry state.Entry) planChange {
	change := planChange{Target: target, Skill: skill, Path: path, DesiredDigest: desiredDigest}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		change.Action = "add"
		return change
	}
	if err != nil {
		change.Action = "conflict"
		change.Reason = err.Error()
		return change
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		change.Action = "conflict"
		change.Reason = "target exists and is not a regular directory"
		return change
	}
	digest, err := contenthash.Directory(path)
	if err != nil {
		change.Action = "conflict"
		change.Reason = err.Error()
		return change
	}
	change.CurrentDigest = digest
	if managed {
		if managedEntry.Target != target || managedEntry.Skill != skill {
			change.Action = "conflict"
			change.Reason = "generation ownership does not match the requested target"
			return change
		}
		if digest != managedEntry.ContentDigest {
			change.Action = "conflict"
			change.Reason = "managed target was modified outside AGX"
			return change
		}
		if digest == desiredDigest {
			change.Action = "unchanged"
			return change
		}
		change.Action = "update"
		return change
	}
	if digest != desiredDigest {
		change.Action = "conflict"
		change.Reason = "unmanaged target content differs"
		return change
	}
	if !adopt {
		change.Action = "conflict"
		change.Reason = "unmanaged target matches; rerun with --adopt to take ownership"
		return change
	}
	change.Action = "adopt"
	return change
}

func planErrorCode(exitCode int) string {
	switch exitCode {
	case ExitLockOutdated:
		return "LOCK_OUTDATED"
	case ExitSourceFailure:
		return "AGX_SOURCE_RESOLUTION_FAILED"
	case ExitInvalidConfig:
		return "AGX_INVALID_CONFIG"
	case ExitAgentUnavailable:
		return "AGX_AGENT_UNAVAILABLE"
	case ExitTargetConflict:
		return "TARGET_CONFLICT"
	default:
		return "AGX_PLAN_FAILED"
	}
}

func renderPlanText(w io.Writer, report planReport) {
	if len(report.Catalogs) == 0 {
		fmt.Fprintf(w, "catalog: %s\n", report.Catalog)
		fmt.Fprintf(w, "lockfile: %s\n", report.Lockfile)
	} else {
		for _, input := range report.Catalogs {
			fmt.Fprintf(w, "catalog %s: %s\n", input.Name, input.Catalog)
			fmt.Fprintf(w, "lockfile %s: %s\n", input.Name, input.Lockfile)
		}
	}
	if report.Profile != "" {
		fmt.Fprintf(w, "profile: %s\n", report.Profile)
	}
	for _, change := range report.Changes {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s", strings.ToUpper(change.Action), change.Target, change.Skill, change.Path)
		if change.Reason != "" {
			fmt.Fprintf(w, "\t%s", change.Reason)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "summary: add=%d adopt=%d update=%d unchanged=%d remove=%d conflict=%d\n", report.Summary.Add, report.Summary.Adopt, report.Summary.Update, report.Summary.Unchanged, report.Summary.Remove, report.Summary.Conflict)
}
