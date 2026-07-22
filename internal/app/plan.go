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
	gitresolver "github.com/paragon-h/agx/internal/resolver/git"
)

const ExitTargetConflict = 5

type planReport struct {
	Catalog  string       `json:"catalog"`
	Lockfile string       `json:"lockfile"`
	Changes  []planChange `json:"changes"`
	Summary  planSummary  `json:"summary"`
}

type planChange struct {
	Target        string `json:"target"`
	Skill         string `json:"skill"`
	Path          string `json:"path"`
	Action        string `json:"action"`
	DesiredDigest string `json:"desiredDigest"`
	CurrentDigest string `json:"currentDigest,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

type planSummary struct {
	Add      int `json:"add"`
	Adopt    int `json:"adopt"`
	Conflict int `json:"conflict"`
}

func (r *Runner) plan(ctx context.Context, args []string) int {
	if helpRequested(args) {
		fmt.Fprintln(r.stdout, "Usage: agx plan [--catalog PATH] [--lockfile PATH] [--adopt] [--json]")
		return ExitSuccess
	}
	flags := flag.NewFlagSet("plan", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	catalogPath := flags.String("catalog", "agx.yaml", "catalog path")
	lockPath := flags.String("lockfile", "", "lockfile path (defaults beside the catalog)")
	adopt := flags.Bool("adopt", false, "allow adoption of unmanaged targets with identical content")
	jsonOutput := flags.Bool("json", false, "emit JSON")
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
	if *lockPath == "" {
		*lockPath = filepath.Join(document.Root, "agx.lock")
	}
	locked, err := lockfile.Load(*lockPath)
	if err != nil {
		return r.commandError(ExitLockOutdated, "AGX_LOCK_INVALID", err)
	}
	if code, err := verifyPlanSources(ctx, document, locked); err != nil {
		return r.commandError(code, planErrorCode(code), err)
	}

	report := planReport{Catalog: document.Path, Lockfile: *lockPath}
	targetPaths := make(map[string]string)
	for _, targetName := range catalogTargets(document.Catalog) {
		adapter, ok := adapterFor(targetName)
		if !ok {
			return r.commandError(ExitAgentUnavailable, "AGX_AGENT_UNAVAILABLE", fmt.Errorf("target %q has no built-in adapter", targetName))
		}
		detection, err := adapter.Detect(ctx)
		if err != nil {
			return r.commandError(ExitAgentUnavailable, "AGX_AGENT_UNAVAILABLE", err)
		}
		if !detection.Installed {
			return r.commandError(ExitAgentUnavailable, "AGX_AGENT_UNAVAILABLE", fmt.Errorf("target %q executable is not available", targetName))
		}
		paths, err := adapter.ResolvePaths(ctx)
		if err != nil {
			return r.commandError(ExitAgentUnavailable, "AGX_AGENT_UNAVAILABLE", err)
		}
		targetPaths[targetName] = paths.SkillsDir
	}
	if err := validateTargetRoots(targetPaths); err != nil {
		return r.commandError(ExitTargetConflict, "TARGET_CONFLICT", err)
	}

	for _, name := range sortedSkillNames(document.Catalog) {
		skill := document.Catalog.Skills[name]
		if skill.Overlay != "" {
			return r.commandError(ExitFailure, "AGX_PLAN_UNSUPPORTED", fmt.Errorf("skill %q uses an overlay; overlay rendering is not implemented yet", name))
		}
		lockedSkill := locked.Skills[name]
		for _, targetName := range enabledTargets(skill.Targets) {
			targetPath := filepath.Join(targetPaths[targetName], name)
			change := inspectPlanTarget(targetName, catalog.QualifiedName(document.Catalog.Metadata.Name, name), targetPath, lockedSkill.ContentDigest, *adopt)
			report.Changes = append(report.Changes, change)
			switch change.Action {
			case "add":
				report.Summary.Add++
			case "adopt":
				report.Summary.Adopt++
			case "conflict":
				report.Summary.Conflict++
			}
		}
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
			digest, err := skillDigest(document.Resolve(skill.Source.Path))
			if err != nil {
				return ExitSourceFailure, fmt.Errorf("skill %q: %w", name, err)
			}
			if digest != lockedSkill.ContentDigest {
				return ExitLockOutdated, fmt.Errorf("local source for %q changed", name)
			}
		case "git":
			if lockedSkill.Source.Type != "git" || lockedSkill.Source.Repository != skill.Source.Repository || lockedSkill.Source.RequestedRevision != skill.Source.Revision || lockedSkill.Source.Path != skill.Source.Path {
				return ExitLockOutdated, fmt.Errorf("Git source for %q differs from lockfile", name)
			}
			resolved, err := gitresolver.New().ResolveSkill(ctx, gitresolver.Request{
				Repository: lockedSkill.Source.Repository,
				Revision:   lockedSkill.Source.ResolvedCommit,
				Path:       lockedSkill.Source.Path,
			})
			if err != nil {
				return ExitSourceFailure, fmt.Errorf("skill %q: %w", name, err)
			}
			if resolved.ResolvedCommit != lockedSkill.Source.ResolvedCommit || resolved.ContentDigest != lockedSkill.ContentDigest {
				return ExitLockOutdated, fmt.Errorf("locked Git content for %q failed verification", name)
			}
		default:
			return ExitInvalidConfig, fmt.Errorf("unsupported source type %q", skill.Source.Type)
		}
		overlayDigest := ""
		if skill.Overlay != "" {
			overlayDigest, err = contenthash.Directory(document.Resolve(skill.Overlay))
			if err != nil {
				return ExitSourceFailure, fmt.Errorf("skill %q overlay: %w", name, err)
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

func inspectPlanTarget(target, skill, path, desiredDigest string, adopt bool) planChange {
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
	default:
		return "AGX_PLAN_FAILED"
	}
}

func renderPlanText(w io.Writer, report planReport) {
	fmt.Fprintf(w, "catalog: %s\n", report.Catalog)
	fmt.Fprintf(w, "lockfile: %s\n", report.Lockfile)
	for _, change := range report.Changes {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s", strings.ToUpper(change.Action), change.Target, change.Skill, change.Path)
		if change.Reason != "" {
			fmt.Fprintf(w, "\t%s", change.Reason)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "summary: add=%d adopt=%d conflict=%d\n", report.Summary.Add, report.Summary.Adopt, report.Summary.Conflict)
}
