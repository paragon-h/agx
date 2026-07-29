package app

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/alanhuangch/agx/internal/security"
)

type auditResult struct {
	Skill     string               `json:"skill"`
	Candidate bool                 `json:"candidate"`
	Digest    string               `json:"digest"`
	Commit    string               `json:"commit,omitempty"`
	Report    security.AuditReport `json:"report"`
}

func (r *Runner) audit(ctx context.Context, args []string) int {
	if helpRequested(args) {
		fmt.Fprintln(r.stdout, "Usage: agx audit <skill> [--catalog PATH] [--lockfile PATH] [--candidate] [--json]")
		return ExitSuccess
	}
	flags := flag.NewFlagSet("audit", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	catalogPath := flags.String("catalog", "", "catalog path (defaults to ./agx.yaml or the active Catalog)")
	lockPath := flags.String("lockfile", "", "lockfile path (defaults beside the catalog)")
	candidate := flags.Bool("candidate", false, "audit the currently resolved catalog revision instead of the lockfile")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	normalized, err := normalizeReviewArgs(args, map[string]bool{"candidate": true, "json": true})
	if err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", err)
	}
	if err := flags.Parse(normalized); err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", err)
	}
	if flags.NArg() != 1 {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", fmt.Errorf("audit requires exactly one skill name"))
	}
	input, err := loadReviewInput(*catalogPath, *lockPath, flags.Arg(0))
	if err != nil {
		return r.commandError(ExitLockOutdated, "AGX_REVIEW_INPUT_INVALID", err)
	}
	version, err := materializeReviewVersion(ctx, input, *candidate)
	if err != nil {
		return r.commandError(ExitSourceFailure, "AGX_REVIEW_SOURCE_FAILED", err)
	}
	defer version.Close()
	report, err := security.AuditDirectory(version.Root)
	if err != nil {
		return r.commandError(ExitFailure, "AGX_AUDIT_FAILED", err)
	}
	result := auditResult{Skill: input.qualifiedName, Candidate: *candidate, Digest: version.Digest, Commit: version.Commit, Report: report}
	if *jsonOutput {
		if err := json.NewEncoder(r.stdout).Encode(result); err != nil {
			return r.commandError(ExitFailure, "AGX_OUTPUT_FAILED", err)
		}
	} else {
		renderAuditText(r.stdout, result)
	}
	if report.Summary.High > 0 {
		return ExitPolicyDenied
	}
	return ExitSuccess
}

func renderAuditText(w io.Writer, result auditResult) {
	version := "locked"
	if result.Candidate {
		version = "candidate"
	}
	fmt.Fprintf(w, "skill: %s (%s)\n", result.Skill, version)
	fmt.Fprintf(w, "digest: %s\n", result.Digest)
	if result.Commit != "" {
		fmt.Fprintf(w, "commit: %s\n", result.Commit)
	}
	for _, finding := range result.Report.Findings {
		location := finding.Path
		if finding.Line > 0 {
			location += fmt.Sprintf(":%d", finding.Line)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", strings.ToUpper(string(finding.Severity)), finding.Code, location, finding.Message)
	}
	fmt.Fprintf(w, "summary: high=%d medium=%d low=%d\n", result.Report.Summary.High, result.Report.Summary.Medium, result.Report.Summary.Low)
}
