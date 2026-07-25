package app

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/paragon-h/agx/internal/catalog"
	"github.com/paragon-h/agx/internal/contenthash"
	"github.com/paragon-h/agx/internal/lockfile"
	"github.com/paragon-h/agx/internal/overlay"
)

type updateReport struct {
	Catalog  string        `json:"catalog"`
	Lockfile string        `json:"lockfile"`
	Accepted bool          `json:"accepted"`
	Updates  []skillUpdate `json:"updates"`
	Summary  updateSummary `json:"summary"`
}

type skillUpdate struct {
	Skill           string `json:"skill"`
	SourceType      string `json:"sourceType"`
	CurrentCommit   string `json:"currentCommit,omitempty"`
	CandidateCommit string `json:"candidateCommit,omitempty"`
	CurrentDigest   string `json:"currentDigest"`
	CandidateDigest string `json:"candidateDigest"`
	Changed         bool   `json:"changed"`
}

type updateSummary struct {
	Changed   int `json:"changed"`
	Unchanged int `json:"unchanged"`
}

type updateContext struct {
	document catalog.Document
	lockPath string
	locked   lockfile.Lockfile
}

func (r *Runner) update(ctx context.Context, args []string) int {
	if helpRequested(args) {
		fmt.Fprintln(r.stdout, "Usage: agx update --check [<skill>] [--catalog PATH] [--lockfile PATH] [--json]")
		fmt.Fprintln(r.stdout, "       agx update <skill> --accept [--catalog PATH] [--lockfile PATH] [--json]")
		return ExitSuccess
	}
	flags := flag.NewFlagSet("update", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	catalogPath := flags.String("catalog", "", "catalog path (defaults to ./agx.yaml or the active Catalog)")
	lockPath := flags.String("lockfile", "", "lockfile path (defaults beside the catalog)")
	check := flags.Bool("check", false, "resolve candidates without changing the lockfile")
	accept := flags.Bool("accept", false, "accept the selected candidate into the lockfile")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	normalized, err := normalizeReviewArgs(args, map[string]bool{"check": true, "accept": true, "json": true})
	if err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", err)
	}
	if err := flags.Parse(normalized); err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", err)
	}
	if *check == *accept {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", fmt.Errorf("choose exactly one of --check or --accept"))
	}
	if flags.NArg() > 1 || (*accept && flags.NArg() != 1) {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", fmt.Errorf("--accept requires exactly one skill; --check accepts at most one"))
	}

	loaded, err := loadUpdateContext(*catalogPath, *lockPath)
	if err != nil {
		return r.commandError(ExitLockOutdated, "AGX_UPDATE_INPUT_INVALID", err)
	}
	names := sortedSkillNames(loaded.document.Catalog)
	if flags.NArg() == 1 {
		shortName, err := updateSkillName(loaded.document.Catalog.Metadata.Name, flags.Arg(0))
		if err != nil {
			return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", err)
		}
		if _, ok := loaded.document.Catalog.Skills[shortName]; !ok {
			return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", fmt.Errorf("skill %q is not declared", flags.Arg(0)))
		}
		names = []string{shortName}
	}
	report := updateReport{Catalog: loaded.document.Path, Lockfile: loaded.lockPath, Accepted: *accept, Updates: []skillUpdate{}}
	var acceptedSkill lockfile.LockedSkill
	for _, name := range names {
		input := reviewInput{
			document:      loaded.document,
			qualifiedName: catalog.QualifiedName(loaded.document.Catalog.Metadata.Name, name),
			skill:         loaded.document.Catalog.Skills[name],
			lockedSkill:   loaded.locked.Skills[name],
		}
		candidate, err := materializeReviewVersion(ctx, input, true)
		if err != nil {
			return r.commandError(ExitSourceFailure, "AGX_UPDATE_SOURCE_FAILED", fmt.Errorf("skill %s: %w", name, err))
		}
		entry := skillUpdate{
			Skill:           input.qualifiedName,
			SourceType:      input.skill.Source.Type,
			CurrentCommit:   input.lockedSkill.Source.ResolvedCommit,
			CandidateCommit: candidate.Commit,
			CurrentDigest:   input.lockedSkill.ContentDigest,
			CandidateDigest: candidate.SourceDigest,
		}
		entry.Changed = entry.CurrentCommit != entry.CandidateCommit || entry.CurrentDigest != entry.CandidateDigest || input.lockedSkill.OverlayDigest != candidate.OverlayDigest
		if entry.Changed {
			report.Summary.Changed++
		} else {
			report.Summary.Unchanged++
		}
		report.Updates = append(report.Updates, entry)
		if *accept {
			acceptedSkill = candidateLockedSkill(input.skill, candidate, time.Now().UTC())
		}
		candidate.Close()
	}
	if *accept && report.Summary.Changed > 0 {
		name, _ := updateSkillName(loaded.document.Catalog.Metadata.Name, flags.Arg(0))
		loaded.locked.Skills[name] = acceptedSkill
		if err := lockfile.Write(loaded.lockPath, loaded.locked); err != nil {
			return r.commandError(ExitFailure, "AGX_UPDATE_WRITE_FAILED", err)
		}
	}
	if *jsonOutput {
		if err := json.NewEncoder(r.stdout).Encode(report); err != nil {
			return r.commandError(ExitFailure, "AGX_OUTPUT_FAILED", err)
		}
		return ExitSuccess
	}
	renderUpdateText(r.stdout, report)
	return ExitSuccess
}

func loadUpdateContext(catalogPath, lockPath string) (updateContext, error) {
	var err error
	catalogPath, err = resolveCatalogPath(catalogPath)
	if err != nil {
		return updateContext{}, err
	}
	document, err := catalog.Load(catalogPath)
	if err != nil {
		return updateContext{}, err
	}
	if lockPath == "" {
		lockPath = filepath.Join(document.Root, "agx.lock")
	}
	locked, err := lockfile.Load(lockPath)
	if err != nil {
		return updateContext{}, err
	}
	digest, err := contenthash.File(document.Path)
	if err != nil {
		return updateContext{}, err
	}
	if digest != locked.CatalogDigest {
		return updateContext{}, fmt.Errorf("catalog content differs from lockfile; run agx lock before checking updates")
	}
	if len(locked.Skills) != len(document.Catalog.Skills) {
		return updateContext{}, fmt.Errorf("catalog and lockfile contain different skill sets")
	}
	for name := range document.Catalog.Skills {
		lockedSkill, ok := locked.Skills[name]
		if !ok {
			return updateContext{}, fmt.Errorf("skill %q is missing from lockfile", name)
		}
		source := document.Catalog.Skills[name].Source
		if source.Type != lockedSkill.Source.Type {
			return updateContext{}, fmt.Errorf("skill %q source type differs from lockfile", name)
		}
		if source.Type == "local" && source.Path != lockedSkill.Source.Path {
			return updateContext{}, fmt.Errorf("skill %q local source differs from lockfile", name)
		}
		if source.Type == "git" && (source.Repository != lockedSkill.Source.Repository || source.Revision != lockedSkill.Source.RequestedRevision || source.Path != lockedSkill.Source.Path) {
			return updateContext{}, fmt.Errorf("skill %q Git source differs from lockfile", name)
		}
		overlayDigest := ""
		if skill := document.Catalog.Skills[name]; skill.Overlay != "" {
			overlayPath, err := document.Resolve(skill.Overlay)
			if err != nil {
				return updateContext{}, fmt.Errorf("skill %q overlay: %w", name, err)
			}
			if err := overlay.Validate(overlayPath); err != nil {
				return updateContext{}, fmt.Errorf("skill %q overlay: %w", name, err)
			}
			overlayDigest, err = contenthash.Directory(overlayPath)
			if err != nil {
				return updateContext{}, fmt.Errorf("skill %q overlay: %w", name, err)
			}
		}
		if overlayDigest != lockedSkill.OverlayDigest {
			return updateContext{}, fmt.Errorf("skill %q overlay differs from lockfile", name)
		}
	}
	return updateContext{document: document, lockPath: lockPath, locked: locked}, nil
}

func updateSkillName(catalogName, value string) (string, error) {
	if !strings.Contains(value, "/") {
		return value, nil
	}
	prefix := catalogName + "/"
	if !strings.HasPrefix(value, prefix) {
		return "", fmt.Errorf("skill %q does not belong to catalog %q", value, catalogName)
	}
	return strings.TrimPrefix(value, prefix), nil
}

func candidateLockedSkill(skill catalog.Skill, candidate reviewVersion, now time.Time) lockfile.LockedSkill {
	locked := lockfile.LockedSkill{
		ContentDigest: candidate.SourceDigest,
		OverlayDigest: candidate.OverlayDigest,
		LockedAt:      now.Format(time.RFC3339),
	}
	if skill.Source.Type == "local" {
		locked.Source = lockfile.LockedSource{Type: "local", Path: skill.Source.Path}
	} else {
		locked.Source = lockfile.LockedSource{
			Type:              "git",
			Repository:        skill.Source.Repository,
			RequestedRevision: skill.Source.Revision,
			ResolvedCommit:    candidate.Commit,
			Path:              skill.Source.Path,
		}
	}
	return locked
}

func renderUpdateText(w io.Writer, report updateReport) {
	for _, update := range report.Updates {
		state := "unchanged"
		if update.Changed {
			state = "update available"
		}
		fmt.Fprintf(w, "%s\t%s\t%s", state, update.Skill, update.CandidateDigest)
		if update.CandidateCommit != "" {
			fmt.Fprintf(w, "\t%s", update.CandidateCommit)
		}
		fmt.Fprintln(w)
	}
	action := "checked"
	if report.Accepted {
		action = "accepted"
	}
	fmt.Fprintf(w, "%s: changed=%d unchanged=%d\n", action, report.Summary.Changed, report.Summary.Unchanged)
}
