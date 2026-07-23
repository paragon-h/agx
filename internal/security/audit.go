package security

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	maxAuditedFileSize = 2 << 20
	largeFileSize      = 1 << 20
)

type Severity string

const (
	SeverityHigh   Severity = "high"
	SeverityMedium Severity = "medium"
	SeverityLow    Severity = "low"
)

type Finding struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	Path     string   `json:"path"`
	Line     int      `json:"line,omitempty"`
	Message  string   `json:"message"`
}

type AuditSummary struct {
	High   int `json:"high"`
	Medium int `json:"medium"`
	Low    int `json:"low"`
}

type AuditReport struct {
	Findings []Finding    `json:"findings"`
	Summary  AuditSummary `json:"summary"`
}

type auditPattern struct {
	severity Severity
	code     string
	message  string
	pattern  *regexp.Regexp
}

var auditPatterns = []auditPattern{
	{SeverityHigh, "download_and_execute", "downloads content and pipes it to a shell", regexp.MustCompile(`(?i)(curl|wget)[^\n|]*\|\s*(sh|bash|zsh)\b`)},
	{SeverityHigh, "destructive_command", "contains a destructive filesystem or Git command", regexp.MustCompile(`(?i)(\brm\s+-[^\n]*r[^\n]*f|\bgit\s+(reset\s+--hard|clean\s+-[fdx]+))`)},
	{SeverityMedium, "credential_access", "references credentials or sensitive configuration", regexp.MustCompile(`(?i)(\.ssh/|id_rsa|aws_access_key|kubeconfig|api[_-]?key|secret|token|password)`)},
	{SeverityMedium, "external_network", "references an external URL or upload operation", regexp.MustCompile(`(?i)(https?://|curl\s+(-X\s+POST|--data|--upload-file))`)},
	{SeverityMedium, "parent_path", "references a path outside the Skill root", regexp.MustCompile(`(^|[\s"'])\.\./`)},
}

func AuditDirectory(root string) (AuditReport, error) {
	report := AuditReport{Findings: []Finding{}}
	manifestFound := false
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.Type()&os.ModeSymlink != 0 {
			addFinding(&report, Finding{Severity: SeverityHigh, Code: "symbolic_link", Path: relative, Message: "symbolic links are not allowed"})
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			addFinding(&report, Finding{Severity: SeverityHigh, Code: "special_file", Path: relative, Message: "special files are not allowed"})
			return nil
		}
		if relative == "SKILL.md" {
			manifestFound = true
		}
		if hiddenPath(relative) {
			addFinding(&report, Finding{Severity: SeverityLow, Code: "hidden_file", Path: relative, Message: "hidden file requires review"})
		}
		if info.Mode()&0o111 != 0 {
			addFinding(&report, Finding{Severity: SeverityHigh, Code: "executable_file", Path: relative, Message: "executable file can run code"})
		}
		if info.Size() > largeFileSize {
			addFinding(&report, Finding{Severity: SeverityMedium, Code: "large_file", Path: relative, Message: fmt.Sprintf("file is larger than %d bytes", largeFileSize)})
		}
		if info.Size() > maxAuditedFileSize {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.IndexByte(data, 0) >= 0 {
			addFinding(&report, Finding{Severity: SeverityHigh, Code: "binary_file", Path: relative, Message: "binary file content cannot be statically reviewed"})
			return nil
		}
		auditText(relative, string(data), &report)
		return nil
	})
	if err != nil {
		return AuditReport{}, err
	}
	if !manifestFound {
		addFinding(&report, Finding{Severity: SeverityHigh, Code: "missing_manifest", Path: "SKILL.md", Message: "required SKILL.md is missing"})
	}
	sort.Slice(report.Findings, func(i, j int) bool {
		if report.Findings[i].Severity != report.Findings[j].Severity {
			return severityRank(report.Findings[i].Severity) > severityRank(report.Findings[j].Severity)
		}
		if report.Findings[i].Path != report.Findings[j].Path {
			return report.Findings[i].Path < report.Findings[j].Path
		}
		if report.Findings[i].Line != report.Findings[j].Line {
			return report.Findings[i].Line < report.Findings[j].Line
		}
		return report.Findings[i].Code < report.Findings[j].Code
	})
	return report, nil
}

func auditText(path, content string, report *AuditReport) {
	lines := strings.Split(content, "\n")
	for index, line := range lines {
		for _, pattern := range auditPatterns {
			if pattern.pattern.MatchString(line) {
				addFinding(report, Finding{Severity: pattern.severity, Code: pattern.code, Path: path, Line: index + 1, Message: pattern.message})
			}
		}
	}
}

func addFinding(report *AuditReport, finding Finding) {
	report.Findings = append(report.Findings, finding)
	switch finding.Severity {
	case SeverityHigh:
		report.Summary.High++
	case SeverityMedium:
		report.Summary.Medium++
	case SeverityLow:
		report.Summary.Low++
	}
}

func hiddenPath(path string) bool {
	for _, component := range strings.Split(path, "/") {
		if strings.HasPrefix(component, ".") && component != "." && component != ".." {
			return true
		}
	}
	return false
}

func severityRank(value Severity) int {
	switch value {
	case SeverityHigh:
		return 3
	case SeverityMedium:
		return 2
	default:
		return 1
	}
}
