package app

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/paragon-h/agx/internal/contenthash"
	"github.com/paragon-h/agx/internal/installer"
	"github.com/paragon-h/agx/internal/state"
)

type statusReport struct {
	State              string             `json:"state"`
	Generation         string             `json:"generation,omitempty"`
	CreatedAt          string             `json:"createdAt,omitempty"`
	PreviousGeneration string             `json:"previousGeneration,omitempty"`
	CatalogDigest      string             `json:"catalogDigest,omitempty"`
	LockfileDigest     string             `json:"lockfileDigest,omitempty"`
	Catalogs           []string           `json:"catalogs,omitempty"`
	Profile            string             `json:"profile,omitempty"`
	Transaction        *installer.Journal `json:"transaction,omitempty"`
	Entries            []statusEntry      `json:"entries"`
	Summary            statusSummary      `json:"summary"`
}

type statusEntry struct {
	Target         string `json:"target"`
	Skill          string `json:"skill"`
	Path           string `json:"path"`
	State          string `json:"state"`
	ExpectedDigest string `json:"expectedDigest"`
	ActualDigest   string `json:"actualDigest,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

type statusSummary struct {
	Healthy  int `json:"healthy"`
	Missing  int `json:"missing"`
	Modified int `json:"modified"`
	Invalid  int `json:"invalid"`
}

func (r *Runner) status(args []string) int {
	if helpRequested(args) {
		fmt.Fprintln(r.stdout, "Usage: agx status [--json]")
		return ExitSuccess
	}
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", err)
	}
	if flags.NArg() != 0 {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", fmt.Errorf("unexpected arguments: %v", flags.Args()))
	}

	current, err := state.Current()
	if err != nil {
		return r.commandError(ExitFailure, "AGX_STATE_INVALID", err)
	}
	journal, err := installer.LoadJournal()
	if err != nil {
		return r.commandError(ExitFailure, "AGX_JOURNAL_INVALID", err)
	}
	report := buildStatusReport(current, journal)
	if *jsonOutput {
		if err := json.NewEncoder(r.stdout).Encode(report); err != nil {
			return r.commandError(ExitFailure, "AGX_OUTPUT_FAILED", err)
		}
	} else {
		renderStatusText(r.stdout, report)
	}
	if journal != nil {
		return ExitFailure
	}
	if report.Summary.Missing+report.Summary.Modified+report.Summary.Invalid > 0 {
		return ExitTargetConflict
	}
	return ExitSuccess
}

func buildStatusReport(current *state.Generation, journal *installer.Journal) statusReport {
	report := statusReport{State: "empty", Transaction: journal, Entries: []statusEntry{}}
	if current != nil {
		report.Generation = current.ID
		report.CreatedAt = current.CreatedAt
		report.PreviousGeneration = current.PreviousID
		report.CatalogDigest = current.CatalogDigest
		report.LockfileDigest = current.LockfileDigest
		report.Catalogs = append([]string(nil), current.Catalogs...)
		report.Profile = current.Profile
		for _, entry := range current.Entries {
			status := inspectStatusEntry(entry)
			report.Entries = append(report.Entries, status)
			switch status.State {
			case "healthy":
				report.Summary.Healthy++
			case "missing":
				report.Summary.Missing++
			case "modified":
				report.Summary.Modified++
			default:
				report.Summary.Invalid++
			}
		}
		report.State = "healthy"
		if report.Summary.Missing+report.Summary.Modified+report.Summary.Invalid > 0 {
			report.State = "drifted"
		}
	}
	if journal != nil {
		report.State = "repair_required"
	}
	return report
}

func inspectStatusEntry(entry state.Entry) statusEntry {
	result := statusEntry{
		Target:         entry.Target,
		Skill:          entry.Skill,
		Path:           entry.Path,
		ExpectedDigest: entry.ContentDigest,
	}
	info, err := os.Lstat(entry.Path)
	if os.IsNotExist(err) {
		result.State = "missing"
		result.Reason = "managed target does not exist"
		return result
	}
	if err != nil {
		result.State = "invalid"
		result.Reason = err.Error()
		return result
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		result.State = "invalid"
		result.Reason = "managed target is not a regular directory"
		return result
	}
	digest, err := contenthash.Directory(entry.Path)
	if err != nil {
		result.State = "invalid"
		result.Reason = err.Error()
		return result
	}
	result.ActualDigest = digest
	if digest != entry.ContentDigest {
		result.State = "modified"
		result.Reason = "managed target content differs from the active generation"
		return result
	}
	result.State = "healthy"
	return result
}

func renderStatusText(w io.Writer, report statusReport) {
	fmt.Fprintf(w, "state: %s\n", report.State)
	if report.Generation == "" {
		fmt.Fprintln(w, "generation: none")
	} else {
		fmt.Fprintf(w, "generation: %s\n", report.Generation)
		fmt.Fprintf(w, "created: %s\n", report.CreatedAt)
		if report.PreviousGeneration != "" {
			fmt.Fprintf(w, "previous: %s\n", report.PreviousGeneration)
		}
		if report.Profile != "" {
			fmt.Fprintf(w, "profile: %s\n", report.Profile)
		}
		if len(report.Catalogs) != 0 {
			fmt.Fprintf(w, "catalogs: %s\n", strings.Join(report.Catalogs, ","))
		}
	}
	if report.Transaction != nil {
		fmt.Fprintf(w, "transaction: %s (%s)\n", report.Transaction.ID, report.Transaction.State)
	}
	for _, entry := range report.Entries {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s", entry.State, entry.Target, entry.Skill, entry.Path)
		if entry.Reason != "" {
			fmt.Fprintf(w, "\t%s", entry.Reason)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "summary: healthy=%d missing=%d modified=%d invalid=%d\n", report.Summary.Healthy, report.Summary.Missing, report.Summary.Modified, report.Summary.Invalid)
}
