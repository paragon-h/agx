package app

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/paragon-h/agx/internal/security"
)

type approveResult struct {
	Skill      string                `json:"skill"`
	ApprovedAt string                `json:"approvedAt"`
	Key        security.ApprovalKey  `json:"key"`
	Audit      security.AuditSummary `json:"audit"`
}

func (r *Runner) approve(ctx context.Context, args []string) int {
	if helpRequested(args) {
		fmt.Fprintln(r.stdout, "Usage: agx approve <skill> [--catalog PATH] [--lockfile PATH] [--allow-risk] [--json]")
		return ExitSuccess
	}
	flags := flag.NewFlagSet("approve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	catalogPath := flags.String("catalog", "agx.yaml", "catalog path")
	lockPath := flags.String("lockfile", "", "lockfile path (defaults beside the catalog)")
	allowRisk := flags.Bool("allow-risk", false, "approve even when the static audit reports high-risk findings")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	normalized, err := normalizeReviewArgs(args, map[string]bool{"allow-risk": true, "json": true})
	if err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", err)
	}
	if err := flags.Parse(normalized); err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", err)
	}
	if flags.NArg() != 1 {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", fmt.Errorf("approve requires exactly one skill name"))
	}
	input, err := loadReviewInput(*catalogPath, *lockPath, flags.Arg(0))
	if err != nil {
		return r.commandError(ExitLockOutdated, "AGX_REVIEW_INPUT_INVALID", err)
	}
	version, err := materializeReviewVersion(ctx, input, false)
	if err != nil {
		return r.commandError(ExitSourceFailure, "AGX_REVIEW_SOURCE_FAILED", err)
	}
	defer version.Close()
	audit, err := security.AuditDirectory(version.Root)
	if err != nil {
		return r.commandError(ExitFailure, "AGX_AUDIT_FAILED", err)
	}
	if audit.Summary.High > 0 && !*allowRisk {
		return r.commandError(ExitPolicyDenied, "AGX_APPROVAL_RISK_REJECTED", fmt.Errorf("audit found %d high-risk finding(s); review them and rerun with --allow-risk to approve explicitly", audit.Summary.High))
	}
	now := time.Now().UTC().Format(time.RFC3339)
	key := security.KeyFor(input.lockedSkill)
	approval := security.Approval{SkillQualifiedName: input.qualifiedName, Key: key, ApprovedAt: now}
	if err := security.SaveApproval(approval); err != nil {
		return r.commandError(ExitFailure, "AGX_APPROVAL_WRITE_FAILED", err)
	}
	result := approveResult{Skill: input.qualifiedName, ApprovedAt: now, Key: key, Audit: audit.Summary}
	if *jsonOutput {
		if err := json.NewEncoder(r.stdout).Encode(result); err != nil {
			return r.commandError(ExitFailure, "AGX_OUTPUT_FAILED", err)
		}
		return ExitSuccess
	}
	fmt.Fprintf(r.stdout, "approved %s at %s: high=%d medium=%d low=%d\n", result.Skill, result.ApprovedAt, result.Audit.High, result.Audit.Medium, result.Audit.Low)
	return ExitSuccess
}
