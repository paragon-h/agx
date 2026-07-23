package app

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/paragon-h/agx/internal/installer"
	"github.com/paragon-h/agx/internal/state"
)

type repairResult struct {
	Repaired    bool   `json:"repaired"`
	Transaction string `json:"transaction,omitempty"`
}

func (r *Runner) repair(args []string) int {
	if helpRequested(args) {
		fmt.Fprintln(r.stdout, "Usage: agx repair [--force] [--json]")
		return ExitSuccess
	}
	flags := flag.NewFlagSet("repair", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	force := flags.Bool("force", false, "replace an existing apply lock after confirming its process has stopped")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", err)
	}
	if flags.NArg() != 0 {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " ")))
	}
	release, err := state.AcquireRepairLock(*force)
	if err != nil {
		return r.commandError(ExitFailure, "AGX_REPAIR_LOCKED", err)
	}
	defer func() {
		if err := release(); err != nil {
			fmt.Fprintf(r.stderr, "AGX_REPAIR_LOCK_CLEANUP: %v\n", err)
		}
	}()
	journal, err := installer.LoadJournal()
	if err != nil {
		return r.commandError(ExitFailure, "AGX_JOURNAL_INVALID", err)
	}
	result := repairResult{}
	if journal != nil {
		result.Transaction = journal.ID
		if err := installer.RepairJournal(journal); err != nil {
			return r.commandError(ExitFailure, "AGX_REPAIR_FAILED", err)
		}
		result.Repaired = true
	}
	if *jsonOutput {
		if err := json.NewEncoder(r.stdout).Encode(result); err != nil {
			return r.commandError(ExitFailure, "AGX_OUTPUT_FAILED", err)
		}
		return ExitSuccess
	}
	if result.Repaired {
		fmt.Fprintf(r.stdout, "repaired transaction %s\n", result.Transaction)
	} else {
		fmt.Fprintln(r.stdout, "no interrupted transaction found")
	}
	return ExitSuccess
}
