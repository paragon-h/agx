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
	"strings"
	"time"

	"github.com/paragon-h/agx/internal/catalog"
	"github.com/paragon-h/agx/internal/contenthash"
	"github.com/paragon-h/agx/internal/installer"
	"github.com/paragon-h/agx/internal/lockfile"
	"github.com/paragon-h/agx/internal/security"
	"github.com/paragon-h/agx/internal/state"
)

type applyResult struct {
	Generation string      `json:"generation,omitempty"`
	Changed    bool        `json:"changed"`
	Summary    planSummary `json:"summary"`
}

type deployment struct {
	change     planChange
	stageRoot  string
	stagePath  string
	backupRoot string
	backupPath string
	backedUp   bool
	installed  bool
}

func (r *Runner) apply(ctx context.Context, args []string) int {
	if helpRequested(args) {
		fmt.Fprintln(r.stdout, "Usage: agx apply [--catalog PATH | --catalogs NAME,...] [--lockfile PATH] [--profile NAME] [--adopt] [--allow-empty] [--json]")
		return ExitSuccess
	}
	flags := flag.NewFlagSet("apply", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	catalogPath := flags.String("catalog", "", "catalog path (defaults to ./agx.yaml or the active Catalog)")
	catalogNames := flags.String("catalogs", "", "comma-separated registered Catalog names to compose")
	lockPath := flags.String("lockfile", "", "lockfile path (defaults beside the catalog)")
	profileName := flags.String("profile", "", "select Skills and targets from a named profile")
	adopt := flags.Bool("adopt", false, "adopt unmanaged targets with identical content")
	allowEmpty := flags.Bool("allow-empty", false, "allow an empty desired selection to remove managed resources")
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
	release, err := state.AcquireApplyLock()
	if err != nil {
		return r.commandError(ExitFailure, "AGX_APPLY_LOCKED", err)
	}
	defer func() {
		if err := release(); err != nil {
			fmt.Fprintf(r.stderr, "AGX_APPLY_LOCK_CLEANUP: %v\n", err)
		}
	}()
	journal, err := installer.LoadJournal()
	if err != nil {
		return r.commandError(ExitFailure, "AGX_JOURNAL_INVALID", err)
	}
	if journal != nil {
		return r.commandError(ExitFailure, "AGX_REPAIR_REQUIRED", fmt.Errorf("unfinished transaction %s is in state %s", journal.ID, journal.State))
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
	if report.Summary.Conflict > 0 {
		if *jsonOutput {
			_ = json.NewEncoder(r.stdout).Encode(report)
		} else {
			renderPlanText(r.stdout, report)
		}
		return ExitTargetConflict
	}
	if report.Summary.Add+report.Summary.Adopt+report.Summary.Update+report.Summary.Remove == 0 {
		result := applyResult{Changed: false, Summary: report.Summary}
		if current != nil {
			result.Generation = current.ID
		}
		return r.renderApplyResult(result, *jsonOutput)
	}

	deployments, err := stageDeployments(ctx, desired, report)
	if err != nil {
		cleanupDeployments(deployments)
		return r.commandError(ExitFailure, "AGX_STAGE_FAILED", err)
	}
	defer cleanupDeployments(deployments)
	if err := preflightDeployments(deployments); err != nil {
		return r.commandError(ExitTargetConflict, "TARGET_CONFLICT", err)
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
		rollbackErr := rollbackWithJournal(deployments, journal)
		if rollbackErr != nil {
			journal.State = installer.StateRepairRequired
			_ = installer.SaveJournal(*journal)
			return r.commandError(ExitFailure, "AGX_ROLLBACK_FAILED", fmt.Errorf("apply failed: %v; rollback failed: %w", err, rollbackErr))
		}
		removeBackups(deployments)
		if deleteErr := installer.DeleteJournal(); deleteErr != nil {
			return r.commandError(ExitFailure, "AGX_JOURNAL_CLEANUP_FAILED", deleteErr)
		}
		return r.commandError(ExitFailure, "AGX_APPLY_FAILED", err)
	}

	generation, err := createGeneration(desired, report, current)
	if err == nil {
		err = state.SaveArtifacts(generation)
	}
	if err == nil {
		err = state.Save(generation)
	}
	if err != nil {
		_ = state.DeleteArtifacts(generation.ID)
		rollbackErr := rollbackWithJournal(deployments, journal)
		if rollbackErr != nil {
			journal.State = installer.StateRepairRequired
			_ = installer.SaveJournal(*journal)
			return r.commandError(ExitFailure, "AGX_ROLLBACK_FAILED", fmt.Errorf("write generation failed: %v; rollback failed: %w", err, rollbackErr))
		}
		removeBackups(deployments)
		if deleteErr := installer.DeleteJournal(); deleteErr != nil {
			return r.commandError(ExitFailure, "AGX_JOURNAL_CLEANUP_FAILED", deleteErr)
		}
		return r.commandError(ExitFailure, "AGX_STATE_WRITE_FAILED", err)
	}
	journal.State = installer.StateCommitted
	if err := installer.SaveJournal(*journal); err != nil {
		return r.commandError(ExitFailure, "AGX_JOURNAL_WRITE_FAILED", err)
	}
	removeBackups(deployments)
	if err := installer.DeleteJournal(); err != nil {
		return r.commandError(ExitFailure, "AGX_JOURNAL_CLEANUP_FAILED", err)
	}
	return r.renderApplyResult(applyResult{Generation: generation.ID, Changed: true, Summary: report.Summary}, *jsonOutput)
}

func requireApprovals(desired desiredState) error {
	for _, skill := range desired.Skills {
		if skill.Skill.Source.Type != "git" || len(enabledTargets(skill.Skill.Targets)) == 0 {
			continue
		}
		approved, err := security.IsApproved(skill.QualifiedName, security.KeyFor(skill.LockedSkill))
		if err != nil {
			return fmt.Errorf("load approval for %s: %w", skill.QualifiedName, err)
		}
		if !approved {
			return fmt.Errorf("%s is not approved for locked commit %s and content %s; run agx audit %s --catalog %s, then agx approve %s --catalog %s", skill.QualifiedName, skill.LockedSkill.Source.ResolvedCommit, skill.LockedSkill.ContentDigest, skill.Name, skill.Document.Path, skill.Name, skill.Document.Path)
		}
	}
	return nil
}

func stageDeployments(ctx context.Context, desired desiredState, report planReport) ([]deployment, error) {
	deployments := make([]deployment, 0, len(report.Changes))
	for _, change := range report.Changes {
		item := deployment{change: change}
		deployments = append(deployments, item)
		if change.Action != "add" && change.Action != "update" && change.Action != "release" {
			continue
		}
		parent := filepath.Dir(change.Path)
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return deployments, err
		}
		stageRoot, err := os.MkdirTemp(parent, ".agx-stage-*")
		if err != nil {
			return deployments, err
		}
		deployments[len(deployments)-1].stageRoot = stageRoot
		stagePath := filepath.Join(stageRoot, "content")
		deployments[len(deployments)-1].stagePath = stagePath
		if change.Kind == "file" {
			if err := os.WriteFile(stagePath, change.content, 0o644); err != nil {
				return deployments, fmt.Errorf("stage %s: %w", change.Skill, err)
			}
		} else {
			skill, exists := desired.skillByQualifiedName(change.Skill)
			if !exists {
				return deployments, fmt.Errorf("planned skill %s is not in the desired state", change.Skill)
			}
			if err := materializeSkill(ctx, skill.Document, skill.Skill, skill.LockedSkill, stagePath); err != nil {
				return deployments, fmt.Errorf("stage %s: %w", change.Skill, err)
			}
		}
		digest, err := contenthash.Path(stagePath, change.Kind)
		if err != nil {
			return deployments, err
		}
		if digest != change.DesiredDigest {
			return deployments, fmt.Errorf("staged digest for %s does not match plan", change.Skill)
		}
	}
	return deployments, nil
}

func materializeSkill(ctx context.Context, document catalog.Document, skill catalog.Skill, locked lockfile.LockedSkill, destination string) error {
	if err := materializeLockedSource(ctx, document, locked, destination); err != nil {
		return err
	}
	if err := applyLockedOverlay(document, skill, locked, destination); err != nil {
		return err
	}
	return nil
}

func preflightDeployments(deployments []deployment) error {
	for _, item := range deployments {
		info, err := os.Lstat(item.change.Path)
		switch item.change.Action {
		case "add":
			if err == nil {
				return fmt.Errorf("target appeared after planning: %s", item.change.Path)
			}
			if !os.IsNotExist(err) {
				return err
			}
		case "remove":
			if os.IsNotExist(err) && item.change.CurrentDigest == "" {
				continue
			}
			fallthrough
		case "update", "release", "adopt", "unchanged":
			if err != nil {
				return fmt.Errorf("target changed after planning: %s: %w", item.change.Path, err)
			}
			if !matchesTargetKind(info, item.change.Kind) {
				return fmt.Errorf("target changed after planning: %s", item.change.Path)
			}
			digest, err := contenthash.Path(item.change.Path, item.change.Kind)
			if err != nil {
				return err
			}
			if digest != item.change.CurrentDigest {
				return fmt.Errorf("target content changed after planning: %s", item.change.Path)
			}
		}
	}
	return nil
}

func prepareJournal(deployments []deployment) (*installer.Journal, error) {
	journal := &installer.Journal{
		ID:    "transaction-" + time.Now().UTC().Format("20060102T150405.000000000Z"),
		State: installer.StatePrepared,
	}
	for i := range deployments {
		item := &deployments[i]
		if item.change.Action != "add" && item.change.Action != "update" && item.change.Action != "release" && item.change.Action != "remove" {
			continue
		}
		if item.change.Action == "remove" && item.change.CurrentDigest == "" {
			continue
		}
		if item.change.Action == "update" || item.change.Action == "release" || item.change.Action == "remove" {
			backupRoot, err := os.MkdirTemp(filepath.Dir(item.change.Path), ".agx-backup-*")
			if err != nil {
				return nil, err
			}
			item.backupRoot = backupRoot
			item.backupPath = filepath.Join(backupRoot, "content")
		}
		journalAction := item.change.Action
		if journalAction == "release" {
			journalAction = "update"
		}
		journal.Targets = append(journal.Targets, installer.TargetChange{
			Agent:         item.change.Target,
			Kind:          item.change.Kind,
			Action:        journalAction,
			TargetPath:    item.change.Path,
			StagePath:     item.stagePath,
			BackupPath:    item.backupPath,
			DesiredDigest: item.change.DesiredDigest,
			CurrentDigest: item.change.CurrentDigest,
		})
	}
	if err := journal.Validate(); err != nil {
		return nil, err
	}
	return journal, nil
}

func switchDeployments(deployments []deployment, journal *installer.Journal) error {
	journalIndex := 0
	recordSwitched := func(item *deployment) error {
		journal.Targets[journalIndex].Switched = true
		journalIndex++
		if err := installer.SaveJournal(*journal); err != nil {
			return fmt.Errorf("record switched target %s: %w", item.change.Path, err)
		}
		return nil
	}
	for i := range deployments {
		item := &deployments[i]
		switch item.change.Action {
		case "add":
			if err := os.Rename(item.stagePath, item.change.Path); err != nil {
				return err
			}
			item.installed = true
			if err := recordSwitched(item); err != nil {
				return err
			}
		case "update", "release", "remove":
			if item.change.Action == "remove" && item.change.CurrentDigest == "" {
				continue
			}
			if err := os.Rename(item.change.Path, item.backupPath); err != nil {
				return err
			}
			item.backedUp = true
			if err := recordSwitched(item); err != nil {
				return err
			}
			if item.change.Action == "update" || item.change.Action == "release" {
				if err := os.Rename(item.stagePath, item.change.Path); err != nil {
					return err
				}
				item.installed = true
			}
		default:
			continue
		}
	}
	return nil
}

func rollbackWithJournal(deployments []deployment, journal *installer.Journal) error {
	journal.State = installer.StateRollingBack
	journalErr := installer.SaveJournal(*journal)
	rollbackErr := rollbackDeploymentsWithProgress(deployments, func(path string) error {
		for i := range journal.Targets {
			if journal.Targets[i].TargetPath == path {
				journal.Targets[i].Restored = true
				return installer.SaveJournal(*journal)
			}
		}
		return fmt.Errorf("restored target %s is missing from transaction journal", path)
	})
	if rollbackErr != nil {
		if journalErr != nil {
			return fmt.Errorf("record rollback: %v; restore targets: %w", journalErr, rollbackErr)
		}
		return rollbackErr
	}
	return journalErr
}

func rollbackDeployments(deployments []deployment) error {
	return rollbackDeploymentsWithProgress(deployments, nil)
}

func rollbackDeploymentsWithProgress(deployments []deployment, restored func(string) error) error {
	var rollbackErrors []string
	for i := len(deployments) - 1; i >= 0; i-- {
		item := &deployments[i]
		changed := false
		if item.installed {
			if err := removeDeploymentTarget(item.change.Path, item.change.Kind); err != nil {
				rollbackErrors = append(rollbackErrors, err.Error())
				continue
			}
			item.installed = false
			changed = true
		}
		if item.backedUp {
			if err := os.Rename(item.backupPath, item.change.Path); err != nil {
				rollbackErrors = append(rollbackErrors, err.Error())
				continue
			}
			item.backedUp = false
			changed = true
		}
		if changed && restored != nil {
			if err := restored(item.change.Path); err != nil {
				rollbackErrors = append(rollbackErrors, err.Error())
			}
		}
	}
	if len(rollbackErrors) > 0 {
		return errors.New(strings.Join(rollbackErrors, "; "))
	}
	return nil
}

func createGeneration(desired desiredState, report planReport, previous *state.Generation) (state.Generation, error) {
	now := time.Now().UTC()
	lockDigest, err := desired.lockfileDigest()
	if err != nil {
		return state.Generation{}, err
	}
	generation := state.Generation{
		ID:             now.Format("20060102T150405.000000000Z"),
		CreatedAt:      now.Format(time.RFC3339Nano),
		CatalogDigest:  desired.catalogDigest(),
		LockfileDigest: lockDigest,
		Catalogs:       desired.catalogNames(),
		Profile:        desired.Profile,
	}
	if previous != nil {
		generation.PreviousID = previous.ID
	}
	for _, change := range report.Changes {
		switch change.Action {
		case "add", "adopt", "update", "unchanged":
			generation.Entries = append(generation.Entries, state.Entry{
				Target:        change.Target,
				Skill:         change.Skill,
				Path:          change.Path,
				Kind:          change.Kind,
				ContentDigest: change.DesiredDigest,
				ManagedDigest: change.ManagedDigest,
			})
		}
	}
	state.SortEntries(generation.Entries)
	state.AssignArtifacts(generation.Entries)
	return generation, nil
}

func matchesTargetKind(info os.FileInfo, kind string) bool {
	if info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	if kind == "file" {
		return info.Mode().IsRegular()
	}
	return info.IsDir()
}

func removeDeploymentTarget(path, kind string) error {
	if kind == "file" {
		return os.Remove(path)
	}
	return os.RemoveAll(path)
}

func cleanupDeployments(deployments []deployment) {
	for _, item := range deployments {
		if item.stageRoot != "" {
			_ = os.RemoveAll(item.stageRoot)
		}
	}
}

func removeBackups(deployments []deployment) {
	for _, item := range deployments {
		if item.backupRoot != "" {
			_ = os.RemoveAll(item.backupRoot)
		}
	}
}

func (r *Runner) renderApplyResult(result applyResult, jsonOutput bool) int {
	if jsonOutput {
		if err := json.NewEncoder(r.stdout).Encode(result); err != nil {
			return r.commandError(ExitFailure, "AGX_OUTPUT_FAILED", err)
		}
		return ExitSuccess
	}
	if !result.Changed {
		fmt.Fprintf(r.stdout, "no changes; generation=%s\n", result.Generation)
		return ExitSuccess
	}
	fmt.Fprintf(r.stdout, "applied generation %s: add=%d adopt=%d update=%d remove=%d\n", result.Generation, result.Summary.Add, result.Summary.Adopt, result.Summary.Update, result.Summary.Remove)
	return ExitSuccess
}
