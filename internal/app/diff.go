package app

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const maxTextDiffSize = 256 << 10

type diffResult struct {
	Skill     string       `json:"skill"`
	Locked    diffVersion  `json:"locked"`
	Candidate diffVersion  `json:"candidate"`
	Files     []fileChange `json:"files"`
	Summary   diffSummary  `json:"summary"`
}

type diffVersion struct {
	Commit string `json:"commit,omitempty"`
	Digest string `json:"digest"`
}

type fileChange struct {
	Path       string `json:"path"`
	Action     string `json:"action"`
	Executable bool   `json:"executable,omitempty"`
	Binary     bool   `json:"binary,omitempty"`
	Patch      string `json:"patch,omitempty"`
}

type diffSummary struct {
	Added    int `json:"added"`
	Modified int `json:"modified"`
	Removed  int `json:"removed"`
}

type fileSnapshot struct {
	data       []byte
	executable bool
}

func (r *Runner) diff(ctx context.Context, args []string) int {
	if helpRequested(args) {
		fmt.Fprintln(r.stdout, "Usage: agx diff <skill> [--catalog PATH] [--lockfile PATH] [--json]")
		return ExitSuccess
	}
	flags := flag.NewFlagSet("diff", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	catalogPath := flags.String("catalog", "", "catalog path (defaults to ./agx.yaml or the active Catalog)")
	lockPath := flags.String("lockfile", "", "lockfile path (defaults beside the catalog)")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	normalized, err := normalizeReviewArgs(args, map[string]bool{"json": true})
	if err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", err)
	}
	if err := flags.Parse(normalized); err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", err)
	}
	if flags.NArg() != 1 {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", fmt.Errorf("diff requires exactly one skill name"))
	}
	input, err := loadReviewInput(*catalogPath, *lockPath, flags.Arg(0))
	if err != nil {
		return r.commandError(ExitLockOutdated, "AGX_REVIEW_INPUT_INVALID", err)
	}
	locked, err := materializeReviewVersion(ctx, input, false)
	if err != nil {
		return r.commandError(ExitSourceFailure, "AGX_REVIEW_SOURCE_FAILED", err)
	}
	defer locked.Close()
	candidate, err := materializeReviewVersion(ctx, input, true)
	if err != nil {
		return r.commandError(ExitSourceFailure, "AGX_REVIEW_SOURCE_FAILED", err)
	}
	defer candidate.Close()
	files, summary, err := buildDirectoryDiff(locked.Root, candidate.Root)
	if err != nil {
		return r.commandError(ExitFailure, "AGX_DIFF_FAILED", err)
	}
	result := diffResult{
		Skill:     input.qualifiedName,
		Locked:    diffVersion{Commit: locked.Commit, Digest: locked.Digest},
		Candidate: diffVersion{Commit: candidate.Commit, Digest: candidate.Digest},
		Files:     files,
		Summary:   summary,
	}
	if *jsonOutput {
		if err := json.NewEncoder(r.stdout).Encode(result); err != nil {
			return r.commandError(ExitFailure, "AGX_OUTPUT_FAILED", err)
		}
		return ExitSuccess
	}
	renderDiffText(r.stdout, result)
	return ExitSuccess
}

func buildDirectoryDiff(leftRoot, rightRoot string) ([]fileChange, diffSummary, error) {
	left, err := snapshotFiles(leftRoot)
	if err != nil {
		return nil, diffSummary{}, err
	}
	right, err := snapshotFiles(rightRoot)
	if err != nil {
		return nil, diffSummary{}, err
	}
	paths := make(map[string]struct{}, len(left)+len(right))
	for path := range left {
		paths[path] = struct{}{}
	}
	for path := range right {
		paths[path] = struct{}{}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	changes := make([]fileChange, 0)
	summary := diffSummary{}
	for _, path := range ordered {
		before, hadBefore := left[path]
		after, hasAfter := right[path]
		change := fileChange{Path: path}
		switch {
		case !hadBefore:
			change.Action = "add"
			summary.Added++
		case !hasAfter:
			change.Action = "remove"
			summary.Removed++
		case bytes.Equal(before.data, after.data) && before.executable == after.executable:
			continue
		default:
			change.Action = "modify"
			summary.Modified++
		}
		leftData := before.data
		rightData := after.data
		change.Executable = before.executable || after.executable
		if len(leftData) > maxTextDiffSize || len(rightData) > maxTextDiffSize || !utf8.Valid(leftData) || !utf8.Valid(rightData) || bytes.IndexByte(leftData, 0) >= 0 || bytes.IndexByte(rightData, 0) >= 0 {
			change.Binary = true
		} else {
			change.Patch = linePatch(path, string(leftData), string(rightData), hadBefore, hasAfter)
		}
		changes = append(changes, change)
	}
	sort.SliceStable(changes, func(i, j int) bool {
		leftPriority := reviewFilePriority(changes[i])
		rightPriority := reviewFilePriority(changes[j])
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		return changes[i].Path < changes[j].Path
	})
	return changes, summary, nil
}

func reviewFilePriority(change fileChange) int {
	if change.Executable {
		return 0
	}
	switch strings.ToLower(filepath.Ext(change.Path)) {
	case ".sh", ".bash", ".zsh", ".fish", ".ps1", ".bat", ".cmd", ".py", ".js", ".ts":
		return 0
	}
	if change.Path == "SKILL.md" {
		return 1
	}
	return 2
}

func snapshotFiles(root string) (map[string]fileSnapshot, error) {
	result := make(map[string]fileSnapshot)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("cannot diff special file %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		result[filepath.ToSlash(relative)] = fileSnapshot{data: data, executable: info.Mode()&0o111 != 0}
		return nil
	})
	return result, err
}

func linePatch(path, before, after string, hadBefore, hasAfter bool) string {
	leftName := "a/" + path
	rightName := "b/" + path
	if !hadBefore {
		leftName = "/dev/null"
	}
	if !hasAfter {
		rightName = "/dev/null"
	}
	leftLines := splitDiffLines(before)
	rightLines := splitDiffLines(after)
	var builder strings.Builder
	fmt.Fprintf(&builder, "--- %s\n+++ %s\n@@ -1,%d +1,%d @@\n", leftName, rightName, len(leftLines), len(rightLines))
	for _, operation := range diffLines(leftLines, rightLines) {
		builder.WriteByte(operation.prefix)
		builder.WriteString(operation.line)
		builder.WriteByte('\n')
	}
	return builder.String()
}

type lineOperation struct {
	prefix byte
	line   string
}

func diffLines(left, right []string) []lineOperation {
	if len(left) > 500 || len(right) > 500 {
		operations := make([]lineOperation, 0, len(left)+len(right))
		for _, line := range left {
			operations = append(operations, lineOperation{'-', line})
		}
		for _, line := range right {
			operations = append(operations, lineOperation{'+', line})
		}
		return operations
	}
	table := make([][]int, len(left)+1)
	for i := range table {
		table[i] = make([]int, len(right)+1)
	}
	for i := len(left) - 1; i >= 0; i-- {
		for j := len(right) - 1; j >= 0; j-- {
			if left[i] == right[j] {
				table[i][j] = table[i+1][j+1] + 1
			} else if table[i+1][j] >= table[i][j+1] {
				table[i][j] = table[i+1][j]
			} else {
				table[i][j] = table[i][j+1]
			}
		}
	}
	operations := make([]lineOperation, 0, len(left)+len(right))
	for i, j := 0, 0; i < len(left) || j < len(right); {
		switch {
		case i < len(left) && j < len(right) && left[i] == right[j]:
			operations = append(operations, lineOperation{' ', left[i]})
			i++
			j++
		case j < len(right) && (i == len(left) || table[i][j+1] > table[i+1][j]):
			operations = append(operations, lineOperation{'+', right[j]})
			j++
		default:
			operations = append(operations, lineOperation{'-', left[i]})
			i++
		}
	}
	return operations
}

func splitDiffLines(value string) []string {
	value = strings.TrimSuffix(value, "\n")
	if value == "" {
		return nil
	}
	return strings.Split(value, "\n")
}

func renderDiffText(w io.Writer, result diffResult) {
	fmt.Fprintf(w, "skill: %s\n", result.Skill)
	fmt.Fprintf(w, "locked: %s", result.Locked.Digest)
	if result.Locked.Commit != "" {
		fmt.Fprintf(w, " (%s)", result.Locked.Commit)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "candidate: %s", result.Candidate.Digest)
	if result.Candidate.Commit != "" {
		fmt.Fprintf(w, " (%s)", result.Candidate.Commit)
	}
	fmt.Fprintln(w)
	for _, file := range result.Files {
		fmt.Fprintf(w, "%s\t%s\n", file.Action, file.Path)
		if file.Binary {
			fmt.Fprintln(w, "  binary or large file; textual diff omitted")
		} else if file.Patch != "" {
			fmt.Fprint(w, file.Patch)
		}
	}
	fmt.Fprintf(w, "summary: added=%d modified=%d removed=%d\n", result.Summary.Added, result.Summary.Modified, result.Summary.Removed)
}
