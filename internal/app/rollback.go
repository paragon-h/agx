package app

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/paragon-h/agx/internal/contenthash"
	"github.com/paragon-h/agx/internal/filetree"
	"github.com/paragon-h/agx/internal/installer"
	"github.com/paragon-h/agx/internal/state"
)

type rollbackResult struct {
	From       string      `json:"from"`
	Restored   string      `json:"restored"`
	Generation string      `json:"generation"`
	Summary    planSummary `json:"summary"`
}

func (r *Runner) rollback(args []string) int {
	if helpRequested(args) {
		fmt.Fprintln(r.stdout, "Usage: agx rollback [--generation ID] [--json]")
		return ExitSuccess
	}
	flags := flag.NewFlagSet("rollback", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	targetID := flags.String("generation", "", "generation to restore (defaults to the previous generation)")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", err)
	}
	if flags.NArg() != 0 {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " ")))
	}

	release, err := state.AcquireApplyLock()
	if err != nil {
		return r.commandError(ExitFailure, "AGX_ROLLBACK_LOCKED", err)
	}
	defer func() {
		if err := release(); err != nil {
			fmt.Fprintf(r.stderr, "AGX_ROLLBACK_LOCK_CLEANUP: %v\n", err)
		}
	}()
	journal, err := installer.LoadJournal()
	if err != nil {
		return r.commandError(ExitFailure, "AGX_JOURNAL_INVALID", err)
	}
	if journal != nil {
		return r.commandError(ExitFailure, "AGX_REPAIR_REQUIRED", fmt.Errorf("unfinished transaction %s is in state %s", journal.ID, journal.State))
	}
	current, err := state.Current()
	if err != nil {
		return r.commandError(ExitFailure, "AGX_STATE_INVALID", err)
	}
	if current == nil {
		return r.commandError(ExitFailure, "AGX_NO_ACTIVE_GENERATION", fmt.Errorf("there is no active generation to roll back"))
	}
	if *targetID == "" {
		*targetID = current.PreviousID
		if *targetID == "" {
			return r.commandError(ExitFailure, "AGX_NO_PREVIOUS_GENERATION", fmt.Errorf("generation %s has no previous generation", current.ID))
		}
	}
	if *targetID == current.ID {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", fmt.Errorf("generation %s is already active", current.ID))
	}
	target, err := state.Load(*targetID)
	if err != nil {
		return r.commandError(ExitFailure, "AGX_GENERATION_INVALID", err)
	}
	report, err := buildRollbackPlan(*current, *target)
	if err != nil {
		return r.commandError(ExitFailure, "AGX_GENERATION_CONTENT_INVALID", err)
	}
	if report.Summary.Conflict > 0 {
		if *jsonOutput {
			_ = json.NewEncoder(r.stdout).Encode(report)
		} else {
			renderPlanText(r.stdout, report)
		}
		return ExitTargetConflict
	}

	deployments, err := stageRollbackDeployments(*target, report)
	if err != nil {
		cleanupDeployments(deployments)
		return r.commandError(ExitFailure, "AGX_STAGE_FAILED", err)
	}
	defer cleanupDeployments(deployments)
	if err := preflightDeployments(deployments); err != nil {
		return r.commandError(ExitTargetConflict, "TARGET_CONFLICT", err)
	}
	generation := createRollbackGeneration(*target, *current)
	mutations := report.Summary.Add + report.Summary.Update + report.Summary.Remove
	if mutations == 0 {
		if err := saveGenerationWithArtifacts(generation); err != nil {
			return r.commandError(ExitFailure, "AGX_STATE_WRITE_FAILED", err)
		}
		return r.renderRollbackResult(rollbackResult{From: current.ID, Restored: target.ID, Generation: generation.ID, Summary: report.Summary}, *jsonOutput)
	}

	journal, err = prepareJournal(deployments)
	if err != nil {
		removeBackups(deployments)
		return r.commandError(ExitFailure, "AGX_JOURNAL_PREPARE_FAILED", err)
	}
	if err := installer.SaveJournal(*journal); err != nil {
		removeBackups(deployments)
		return r.commandError(ExitFailure, "AGX_JOURNAL_WRITE_FAILED", err)
	}
	journal.State = installer.StateApplying
	if err := installer.SaveJournal(*journal); err != nil {
		_ = installer.DeleteJournal()
		removeBackups(deployments)
		return r.commandError(ExitFailure, "AGX_JOURNAL_WRITE_FAILED", err)
	}
	if err := switchDeployments(deployments, journal); err != nil {
		return r.rollbackCommandFailure(deployments, journal, "switch targets", err)
	}
	if err := saveGenerationWithArtifacts(generation); err != nil {
		return r.rollbackCommandFailure(deployments, journal, "write generation", err)
	}
	journal.State = installer.StateCommitted
	if err := installer.SaveJournal(*journal); err != nil {
		return r.commandError(ExitFailure, "AGX_JOURNAL_WRITE_FAILED", err)
	}
	removeBackups(deployments)
	if err := installer.DeleteJournal(); err != nil {
		return r.commandError(ExitFailure, "AGX_JOURNAL_CLEANUP_FAILED", err)
	}
	return r.renderRollbackResult(rollbackResult{From: current.ID, Restored: target.ID, Generation: generation.ID, Summary: report.Summary}, *jsonOutput)
}

func buildRollbackPlan(current, target state.Generation) (planReport, error) {
	report := planReport{Catalog: target.ID, Lockfile: target.LockfileDigest}
	currentByPath := current.ManagedByPath()
	targetByPath := target.ManagedByPath()
	for _, desired := range target.Entries {
		artifact, err := state.ArtifactPath(target.ID, desired.Artifact)
		if err != nil {
			return planReport{}, fmt.Errorf("generation %s cannot restore %s: %w", target.ID, desired.Skill, err)
		}
		digest, err := contenthash.Directory(artifact)
		if err != nil {
			return planReport{}, fmt.Errorf("generation %s artifact for %s: %w", target.ID, desired.Skill, err)
		}
		if digest != desired.ContentDigest {
			return planReport{}, fmt.Errorf("generation %s artifact for %s has an unexpected digest", target.ID, desired.Skill)
		}
		change := planChange{Target: desired.Target, Skill: desired.Skill, Path: desired.Path, DesiredDigest: desired.ContentDigest}
		if existing, ok := currentByPath[filepath.Clean(desired.Path)]; ok {
			change.CurrentDigest = inspectRollbackCurrent(existing, &change)
			if change.Action == "conflict" {
				report.Summary.Conflict++
			} else if existing.Target != desired.Target || existing.Skill != desired.Skill {
				change.Action = "conflict"
				change.Reason = "generation ownership differs"
				report.Summary.Conflict++
			} else if existing.ContentDigest == desired.ContentDigest {
				change.Action = "unchanged"
				report.Summary.Unchanged++
			} else {
				change.Action = "update"
				report.Summary.Update++
			}
		} else if _, err := os.Lstat(desired.Path); os.IsNotExist(err) {
			change.Action = "add"
			report.Summary.Add++
		} else if err != nil {
			change.Action = "conflict"
			change.Reason = err.Error()
			report.Summary.Conflict++
		} else {
			change.Action = "conflict"
			change.Reason = "target is not managed by the active generation"
			report.Summary.Conflict++
		}
		report.Changes = append(report.Changes, change)
	}
	for _, existing := range current.Entries {
		if _, ok := targetByPath[filepath.Clean(existing.Path)]; ok {
			continue
		}
		change := planChange{Target: existing.Target, Skill: existing.Skill, Path: existing.Path, Action: "remove"}
		change.CurrentDigest = inspectRollbackCurrent(existing, &change)
		if change.Action == "conflict" {
			report.Summary.Conflict++
		} else {
			report.Summary.Remove++
		}
		report.Changes = append(report.Changes, change)
	}
	sort.Slice(report.Changes, func(i, j int) bool {
		if report.Changes[i].Target != report.Changes[j].Target {
			return report.Changes[i].Target < report.Changes[j].Target
		}
		return report.Changes[i].Skill < report.Changes[j].Skill
	})
	return report, nil
}

func inspectRollbackCurrent(entry state.Entry, change *planChange) string {
	info, err := os.Lstat(entry.Path)
	if err != nil {
		change.Action = "conflict"
		change.Reason = "active managed target is missing or unreadable"
		return ""
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		change.Action = "conflict"
		change.Reason = "active managed target is not a regular directory"
		return ""
	}
	digest, err := contenthash.Directory(entry.Path)
	if err != nil {
		change.Action = "conflict"
		change.Reason = err.Error()
		return ""
	}
	if digest != entry.ContentDigest {
		change.Action = "conflict"
		change.Reason = "managed target was modified outside AGX"
	}
	return digest
}

func stageRollbackDeployments(target state.Generation, report planReport) ([]deployment, error) {
	targetByPath := target.ManagedByPath()
	deployments := make([]deployment, 0, len(report.Changes))
	for _, change := range report.Changes {
		deployments = append(deployments, deployment{change: change})
		if change.Action != "add" && change.Action != "update" {
			continue
		}
		entry := targetByPath[filepath.Clean(change.Path)]
		artifact, err := state.ArtifactPath(target.ID, entry.Artifact)
		if err != nil {
			return deployments, err
		}
		if err := os.MkdirAll(filepath.Dir(change.Path), 0o755); err != nil {
			return deployments, err
		}
		stageRoot, err := os.MkdirTemp(filepath.Dir(change.Path), ".agx-stage-*")
		if err != nil {
			return deployments, err
		}
		item := &deployments[len(deployments)-1]
		item.stageRoot = stageRoot
		item.stagePath = filepath.Join(stageRoot, "content")
		if err := filetree.Copy(artifact, item.stagePath); err != nil {
			return deployments, err
		}
	}
	return deployments, nil
}

func createRollbackGeneration(target, current state.Generation) state.Generation {
	now := time.Now().UTC()
	generation := state.Generation{
		ID:             now.Format("20060102T150405.000000000Z"),
		CreatedAt:      now.Format(time.RFC3339Nano),
		CatalogDigest:  target.CatalogDigest,
		LockfileDigest: target.LockfileDigest,
		Catalogs:       append([]string(nil), target.Catalogs...),
		Profile:        target.Profile,
		PreviousID:     current.ID,
	}
	for _, entry := range target.Entries {
		entry.Artifact = ""
		generation.Entries = append(generation.Entries, entry)
	}
	state.SortEntries(generation.Entries)
	state.AssignArtifacts(generation.Entries)
	return generation
}

func saveGenerationWithArtifacts(generation state.Generation) error {
	if err := state.SaveArtifacts(generation); err != nil {
		return err
	}
	if err := state.Save(generation); err != nil {
		_ = state.DeleteArtifacts(generation.ID)
		return err
	}
	return nil
}

func (r *Runner) rollbackCommandFailure(deployments []deployment, journal *installer.Journal, operation string, operationErr error) int {
	rollbackErr := rollbackWithJournal(deployments, journal)
	if rollbackErr != nil {
		journal.State = installer.StateRepairRequired
		_ = installer.SaveJournal(*journal)
		return r.commandError(ExitFailure, "AGX_ROLLBACK_FAILED", fmt.Errorf("%s failed: %v; compensation failed: %w", operation, operationErr, rollbackErr))
	}
	removeBackups(deployments)
	if err := installer.DeleteJournal(); err != nil {
		return r.commandError(ExitFailure, "AGX_JOURNAL_CLEANUP_FAILED", err)
	}
	return r.commandError(ExitFailure, "AGX_ROLLBACK_APPLY_FAILED", fmt.Errorf("%s: %w", operation, operationErr))
}

func (r *Runner) renderRollbackResult(result rollbackResult, jsonOutput bool) int {
	if jsonOutput {
		if err := json.NewEncoder(r.stdout).Encode(result); err != nil {
			return r.commandError(ExitFailure, "AGX_OUTPUT_FAILED", err)
		}
		return ExitSuccess
	}
	fmt.Fprintf(r.stdout, "restored generation %s as %s: add=%d update=%d remove=%d\n", result.Restored, result.Generation, result.Summary.Add, result.Summary.Update, result.Summary.Remove)
	return ExitSuccess
}
