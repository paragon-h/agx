package app

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/paragon-h/agx/internal/adapters"
	"github.com/paragon-h/agx/internal/adapters/claude"
	"github.com/paragon-h/agx/internal/adapters/codex"
	"github.com/paragon-h/agx/internal/adapters/opencode"
	"github.com/paragon-h/agx/internal/adapters/pi"
	"github.com/paragon-h/agx/internal/catalog"
	"github.com/paragon-h/agx/internal/installer"
	"github.com/paragon-h/agx/internal/state"
)

type doctorReport struct {
	Catalog     string             `json:"catalog"`
	Targets     []doctorTarget     `json:"targets"`
	Transaction *installer.Journal `json:"transaction,omitempty"`
	ApplyLock   *state.ApplyLock   `json:"applyLock,omitempty"`
	Recovery    string             `json:"recovery,omitempty"`
}

type doctorTarget struct {
	Name            string `json:"name"`
	Installed       bool   `json:"installed"`
	Executable      string `json:"executable,omitempty"`
	SkillsDir       string `json:"skillsDir"`
	SkillsDirExists bool   `json:"skillsDirExists"`
	Error           string `json:"error,omitempty"`
}

func (r *Runner) doctor(ctx context.Context, args []string) int {
	if helpRequested(args) {
		fmt.Fprintln(r.stdout, "Usage: agx doctor [--catalog PATH] [--json]")
		return ExitSuccess
	}
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	catalogPath := flags.String("catalog", "", "catalog path (defaults to ./agx.yaml or the active Catalog)")
	jsonOutput := flags.Bool("json", false, "emit JSON")
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
	targetNames := catalogTargets(document.Catalog)
	journal, err := installer.LoadJournal()
	if err != nil {
		return r.commandError(ExitFailure, "AGX_JOURNAL_INVALID", err)
	}
	applyLock, err := state.InspectApplyLock()
	if err != nil {
		return r.commandError(ExitFailure, "AGX_APPLY_LOCK_INVALID", err)
	}
	report := doctorReport{Catalog: document.Path, Targets: make([]doctorTarget, 0, len(targetNames)), Transaction: journal, ApplyLock: applyLock}
	if journal != nil {
		report.Recovery = "after confirming no AGX process is active, run: agx repair"
	}
	unavailable := false
	repairRequired := journal != nil
	for _, targetName := range targetNames {
		adapter, ok := adapterFor(targetName)
		if !ok {
			unavailable = true
			report.Targets = append(report.Targets, doctorTarget{Name: targetName, Error: "no built-in adapter"})
			continue
		}
		entry := doctorTarget{Name: targetName}
		detection, detectErr := adapter.Detect(ctx)
		if detectErr != nil {
			entry.Error = appendDiagnostic(entry.Error, detectErr.Error())
			unavailable = true
		} else {
			entry.Installed = detection.Installed
			entry.Executable = detection.Executable
			if !detection.Installed {
				unavailable = true
			}
		}
		paths, pathErr := adapter.ResolvePaths(ctx)
		if pathErr != nil {
			entry.Error = appendDiagnostic(entry.Error, pathErr.Error())
			unavailable = true
		} else {
			entry.SkillsDir = paths.SkillsDir
			if info, statErr := os.Stat(paths.SkillsDir); statErr == nil {
				entry.SkillsDirExists = info.IsDir()
				if !info.IsDir() {
					entry.Error = appendDiagnostic(entry.Error, "skills path exists but is not a directory")
					unavailable = true
				}
			} else if !os.IsNotExist(statErr) {
				entry.Error = appendDiagnostic(entry.Error, statErr.Error())
				unavailable = true
			}
		}
		report.Targets = append(report.Targets, entry)
	}
	if *jsonOutput {
		if err := json.NewEncoder(r.stdout).Encode(report); err != nil {
			return r.commandError(ExitFailure, "AGX_OUTPUT_FAILED", err)
		}
	} else {
		renderDoctorText(r.stdout, report)
	}
	if repairRequired {
		return ExitFailure
	}
	if unavailable {
		return ExitAgentUnavailable
	}
	return ExitSuccess
}

func appendDiagnostic(current, next string) string {
	if current == "" {
		return next
	}
	return current + "; " + next
}

func catalogTargets(value catalog.Catalog) []string {
	seen := make(map[string]struct{})
	for _, skill := range value.Skills {
		for target, config := range skill.Targets {
			if config.Enabled == nil || *config.Enabled {
				seen[target] = struct{}{}
			}
		}
	}
	targets := make([]string, 0, len(seen))
	for target := range seen {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	return targets
}

func adapterFor(name string) (adapters.Adapter, bool) {
	switch name {
	case "codex":
		return codex.New(), true
	case "claude":
		return claude.New(), true
	case "pi":
		return pi.New(), true
	case "opencode":
		return opencode.New(), true
	default:
		return nil, false
	}
}

func renderDoctorText(w io.Writer, report doctorReport) {
	fmt.Fprintf(w, "catalog: %s\n", report.Catalog)
	if report.ApplyLock != nil {
		fmt.Fprintf(w, "apply lock: pid=%d path=%s\n", report.ApplyLock.PID, report.ApplyLock.Path)
	}
	if report.Transaction != nil {
		fmt.Fprintf(w, "transaction: %s (%s)\n", report.Transaction.ID, report.Transaction.State)
	}
	if report.Recovery != "" {
		fmt.Fprintf(w, "recovery: %s\n", report.Recovery)
	}
	for _, target := range report.Targets {
		fmt.Fprintf(w, "target %s:\n", target.Name)
		if target.Installed {
			fmt.Fprintf(w, "  executable: %s (installed)\n", target.Executable)
		} else {
			fmt.Fprintln(w, "  executable: missing")
		}
		if target.SkillsDir != "" {
			state := "missing"
			if target.SkillsDirExists {
				state = "exists"
			}
			fmt.Fprintf(w, "  skills: %s (%s)\n", target.SkillsDir, state)
		}
		if target.Error != "" {
			fmt.Fprintf(w, "  error: %s\n", target.Error)
		}
	}
}
