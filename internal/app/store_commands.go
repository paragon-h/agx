package app

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	contentstore "github.com/alanhuangch/agx/internal/store"
)

type storeSummary struct {
	Objects           int   `json:"objects"`
	Referenced        int   `json:"referenced"`
	Unreferenced      int   `json:"unreferenced"`
	Bytes             int64 `json:"bytes"`
	ReferencedBytes   int64 `json:"referencedBytes"`
	UnreferencedBytes int64 `json:"unreferencedBytes"`
	Missing           int   `json:"missing"`
	Corrupt           int   `json:"corrupt"`
	ScanIssues        int   `json:"scanIssues"`
}

type storeReport struct {
	Root       string                   `json:"root"`
	References int                      `json:"references"`
	Stale      int                      `json:"staleReferences"`
	Summary    storeSummary             `json:"summary"`
	Missing    []string                 `json:"missing,omitempty"`
	Corrupt    []string                 `json:"corrupt,omitempty"`
	Issues     []contentstore.ScanIssue `json:"issues,omitempty"`
}

type storeGCReport struct {
	storeReport
	DryRun           bool     `json:"dryRun"`
	StaleCandidates  int      `json:"staleCandidates"`
	PrunedReferences int      `json:"prunedReferences"`
	Candidates       []string `json:"candidates,omitempty"`
	Removed          []string `json:"removed,omitempty"`
	ReclaimedBytes   int64    `json:"reclaimedBytes"`
}

type storeInspection struct {
	report     storeReport
	objects    []contentstore.Object
	references []contentstore.ReferenceRecord
	referenced map[string]struct{}
	stale      []contentstore.ReferenceRecord
}

func (r *Runner) storeCommand(args []string) int {
	if len(args) == 0 || helpRequested(args) {
		r.writeStoreHelp(r.stdout)
		return ExitSuccess
	}
	switch args[0] {
	case "status":
		return r.storeStatus(args[1:])
	case "verify":
		return r.storeVerify(args[1:])
	case "gc":
		return r.storeGC(args[1:])
	default:
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", fmt.Errorf("unknown store command %q", args[0]))
	}
}

func (r *Runner) storeStatus(args []string) int {
	if helpRequested(args) {
		fmt.Fprintln(r.stdout, "Usage: agx store status [--json]")
		return ExitSuccess
	}
	jsonOutput, ok := parseStoreJSONFlag("store status", args)
	if !ok {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", errors.New("invalid store status arguments"))
	}
	inspection, err := inspectStore()
	if err != nil {
		return r.commandError(ExitFailure, "AGX_STORE_INVALID", err)
	}
	if jsonOutput {
		if err := json.NewEncoder(r.stdout).Encode(inspection.report); err != nil {
			return r.commandError(ExitFailure, "AGX_OUTPUT_FAILED", err)
		}
	} else {
		renderStoreReport(r.stdout, inspection.report)
	}
	if inspection.report.Summary.Missing > 0 || inspection.report.Summary.Corrupt > 0 || inspection.report.Summary.ScanIssues > 0 {
		return ExitFailure
	}
	return ExitSuccess
}

func (r *Runner) storeVerify(args []string) int {
	if helpRequested(args) {
		fmt.Fprintln(r.stdout, "Usage: agx store verify [--json]")
		return ExitSuccess
	}
	jsonOutput, ok := parseStoreJSONFlag("store verify", args)
	if !ok {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", errors.New("invalid store verify arguments"))
	}
	inspection, err := inspectStore()
	if err != nil {
		return r.commandError(ExitFailure, "AGX_STORE_INVALID", err)
	}
	if jsonOutput {
		if err := json.NewEncoder(r.stdout).Encode(inspection.report); err != nil {
			return r.commandError(ExitFailure, "AGX_OUTPUT_FAILED", err)
		}
	} else if inspection.report.Summary.Missing == 0 && inspection.report.Summary.Corrupt == 0 && inspection.report.Summary.ScanIssues == 0 {
		fmt.Fprintf(r.stdout, "verified %d Store object(s) across %d lock reference(s)\n", inspection.report.Summary.Objects, inspection.report.References)
	} else {
		renderStoreReport(r.stdout, inspection.report)
	}
	if inspection.report.Summary.Missing > 0 || inspection.report.Summary.Corrupt > 0 || inspection.report.Summary.ScanIssues > 0 {
		return ExitFailure
	}
	return ExitSuccess
}

func (r *Runner) storeGC(args []string) int {
	flags := flag.NewFlagSet("store gc", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dryRun := flags.Bool("dry-run", false, "report candidates without removing objects")
	pruneStale := flags.Bool("prune-stale", false, "remove references whose lockfile no longer exists")
	force := flags.Bool("force", false, "allow removing objects when no live lock references exist")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if helpRequested(args) {
		fmt.Fprintln(r.stdout, "Usage: agx store gc [--dry-run] [--prune-stale] [--force] [--json]")
		return ExitSuccess
	}
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		if err == nil {
			err = fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
		}
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", err)
	}
	inspection, err := inspectStore()
	if err != nil {
		return r.commandError(ExitFailure, "AGX_STORE_INVALID", err)
	}
	staleCandidates := 0
	if *pruneStale {
		staleCandidates = len(inspection.stale)
	}
	if inspection.report.Summary.Missing > 0 || inspection.report.Summary.Corrupt > 0 || inspection.report.Summary.ScanIssues > 0 {
		if *jsonOutput {
			_ = json.NewEncoder(r.stdout).Encode(storeGCReport{storeReport: inspection.report, DryRun: *dryRun, StaleCandidates: staleCandidates})
		}
		return r.commandError(ExitFailure, "AGX_STORE_INVALID", errors.New("Store verification failed; refusing to remove objects"))
	}
	if *pruneStale && !*dryRun && !*force && inspection.report.Summary.Objects > 0 && len(inspection.stale) == inspection.report.References {
		return r.commandError(ExitFailure, "AGX_STORE_GC_REQUIRES_ROOTS", errors.New("pruning would leave no live lock references; rerun with --force after confirming these objects are disposable"))
	}
	pruned := 0
	if *pruneStale && !*dryRun {
		for _, reference := range inspection.stale {
			if err := contentstore.RemoveReference(reference.Lockfile); err != nil {
				return r.commandError(ExitFailure, "AGX_STORE_REFERENCE_REMOVE_FAILED", err)
			}
			pruned++
		}
		inspection, err = inspectStore()
		if err != nil {
			return r.commandError(ExitFailure, "AGX_STORE_INVALID", err)
		}
		if inspection.report.Summary.Missing > 0 || inspection.report.Summary.Corrupt > 0 || inspection.report.Summary.ScanIssues > 0 {
			return r.commandError(ExitFailure, "AGX_STORE_INVALID", errors.New("Store changed during garbage collection; refusing to remove objects"))
		}
	}
	if !*dryRun && !*force && inspection.report.References == 0 && inspection.report.Summary.Objects > 0 {
		return r.commandError(ExitFailure, "AGX_STORE_GC_REQUIRES_ROOTS", errors.New("no live lock references exist; rerun with --force after confirming these objects are disposable"))
	}
	gcReferenced := inspection.referenced
	if *dryRun && *pruneStale {
		gcReferenced = make(map[string]struct{})
		stale := make(map[string]struct{}, len(inspection.stale))
		for _, reference := range inspection.stale {
			stale[reference.Manifest] = struct{}{}
		}
		for _, reference := range inspection.references {
			if _, ok := stale[reference.Manifest]; ok {
				continue
			}
			for _, digest := range reference.Digests {
				gcReferenced[digest] = struct{}{}
			}
		}
	}
	candidates := make([]contentstore.Object, 0)
	for _, object := range inspection.objects {
		if _, ok := gcReferenced[object.Digest]; !ok {
			candidates = append(candidates, object)
		}
	}
	result := storeGCReport{storeReport: inspection.report, DryRun: *dryRun, StaleCandidates: staleCandidates, PrunedReferences: pruned, Candidates: make([]string, 0, len(candidates)), Removed: []string{}}
	for _, object := range candidates {
		result.Candidates = append(result.Candidates, object.Digest)
		if *dryRun {
			continue
		}
		if err := contentstore.Remove(object.Digest); err != nil {
			return r.commandError(ExitFailure, "AGX_STORE_REMOVE_FAILED", err)
		}
		result.Removed = append(result.Removed, object.Digest)
		result.ReclaimedBytes += object.Size
	}
	if *jsonOutput {
		if err := json.NewEncoder(r.stdout).Encode(result); err != nil {
			return r.commandError(ExitFailure, "AGX_OUTPUT_FAILED", err)
		}
		return ExitSuccess
	}
	if *dryRun {
		fmt.Fprintf(r.stdout, "Store GC dry-run: %d object candidate(s), %d stale reference candidate(s)\n", len(result.Candidates), result.StaleCandidates)
	} else {
		fmt.Fprintf(r.stdout, "Store GC removed %d object(s), reclaimed %d byte(s), pruned %d stale reference(s)\n", len(result.Removed), result.ReclaimedBytes, result.PrunedReferences)
	}
	return ExitSuccess
}

func inspectStore() (storeInspection, error) {
	root, err := contentstore.Root()
	if err != nil {
		return storeInspection{}, err
	}
	objects, issues, err := contentstore.Objects()
	if err != nil {
		return storeInspection{}, err
	}
	references, err := contentstore.References()
	if err != nil {
		return storeInspection{}, err
	}
	referenced := make(map[string]struct{})
	for _, reference := range references {
		for _, digest := range reference.Digests {
			referenced[digest] = struct{}{}
		}
	}
	objectByDigest := make(map[string]contentstore.Object, len(objects))
	for _, object := range objects {
		objectByDigest[object.Digest] = object
	}
	corrupt := make([]string, 0)
	for _, object := range objects {
		if err := contentstore.Verify(object.Digest); err != nil {
			corrupt = append(corrupt, object.Digest)
		}
	}
	missing := make([]string, 0)
	for digest := range referenced {
		if _, ok := objectByDigest[digest]; !ok {
			missing = append(missing, digest)
		}
	}
	sort.Strings(corrupt)
	sort.Strings(missing)
	stale := make([]contentstore.ReferenceRecord, 0)
	for _, reference := range references {
		if _, err := os.Stat(reference.Lockfile); os.IsNotExist(err) {
			stale = append(stale, reference)
		}
	}
	report := storeReport{Root: root, References: len(references), Stale: len(stale), Missing: missing, Corrupt: corrupt, Issues: issues}
	report.Summary.Objects = len(objects)
	report.Summary.Missing = len(missing)
	report.Summary.Corrupt = len(corrupt)
	report.Summary.ScanIssues = len(issues)
	for _, object := range objects {
		report.Summary.Bytes += object.Size
		if _, ok := referenced[object.Digest]; ok {
			report.Summary.Referenced++
			report.Summary.ReferencedBytes += object.Size
		} else {
			report.Summary.Unreferenced++
			report.Summary.UnreferencedBytes += object.Size
		}
	}
	return storeInspection{report: report, objects: objects, references: references, referenced: referenced, stale: stale}, nil
}

func parseStoreJSONFlag(name string, args []string) (bool, bool) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return false, false
	}
	return *jsonOutput, true
}

func renderStoreReport(w io.Writer, report storeReport) {
	fmt.Fprintf(w, "root: %s\n", report.Root)
	fmt.Fprintf(w, "references: %d (stale=%d)\n", report.References, report.Stale)
	fmt.Fprintf(w, "summary: objects=%d referenced=%d unreferenced=%d bytes=%d missing=%d corrupt=%d scanIssues=%d\n", report.Summary.Objects, report.Summary.Referenced, report.Summary.Unreferenced, report.Summary.Bytes, report.Summary.Missing, report.Summary.Corrupt, report.Summary.ScanIssues)
	for _, digest := range report.Missing {
		fmt.Fprintf(w, "missing: %s\n", digest)
	}
	for _, digest := range report.Corrupt {
		fmt.Fprintf(w, "corrupt: %s\n", digest)
	}
	for _, issue := range report.Issues {
		fmt.Fprintf(w, "issue: %s\t%s\n", issue.Path, issue.Error)
	}
}

func (r *Runner) writeStoreHelp(w io.Writer) {
	fmt.Fprintln(w, `Usage:
  agx store status [--json]
  agx store verify [--json]
  agx store gc [--dry-run] [--prune-stale] [--force] [--json]`)
}
