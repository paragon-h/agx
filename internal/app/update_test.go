package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paragon-h/agx/internal/lockfile"
	"github.com/paragon-h/agx/internal/security"
)

func TestRunnerUpdateCheckAndAcceptGitCandidate(t *testing.T) {
	repository := t.TempDir()
	runGitCommand(t, repository, "init", "--quiet", "--initial-branch=main")
	runGitCommand(t, repository, "config", "user.name", "AGX Test")
	runGitCommand(t, repository, "config", "user.email", "agx@example.invalid")
	skillRoot := filepath.Join(repository, "skills", "review")
	if err := os.MkdirAll(skillRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(skillRoot, "SKILL.md")
	if err := os.WriteFile(manifest, []byte("# Version one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, repository, "add", ".")
	runGitCommand(t, repository, "commit", "--quiet", "-m", "version one")
	firstCommit := strings.TrimSpace(runGitCommand(t, repository, "rev-parse", "HEAD"))

	catalogRoot := t.TempDir()
	catalogPath := filepath.Join(catalogRoot, "agx.yaml")
	catalogYAML := fmt.Sprintf(`apiVersion: agx.dev/v1alpha1
kind: Catalog
metadata:
  name: personal
skills:
  review:
    source:
      type: git
      repository: %q
      revision: main
      path: skills/review
    targets:
      codex: {}
`, repository)
	if err := os.WriteFile(catalogPath, []byte(catalogYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGX_STATE_HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	runner := New(&stdout, &stderr, "dev")
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("lock code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"approve", "review", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("approve code = %d, stderr = %q", code, stderr.String())
	}

	if err := os.WriteFile(manifest, []byte("# Version two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, repository, "add", ".")
	runGitCommand(t, repository, "commit", "--quiet", "-m", "version two")
	secondCommit := strings.TrimSpace(runGitCommand(t, repository, "rev-parse", "HEAD"))

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"update", "--check", "--catalog", catalogPath, "--json"}); code != ExitSuccess {
		t.Fatalf("update check code = %d, stderr = %q", code, stderr.String())
	}
	var checked updateReport
	if err := json.Unmarshal(stdout.Bytes(), &checked); err != nil {
		t.Fatal(err)
	}
	if checked.Summary.Changed != 1 || checked.Updates[0].CurrentCommit != firstCommit || checked.Updates[0].CandidateCommit != secondCommit {
		t.Fatalf("update check = %#v", checked)
	}
	lockPath := filepath.Join(catalogRoot, "agx.lock")
	beforeAccept, err := lockfile.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if beforeAccept.Skills["review"].Source.ResolvedCommit != firstCommit {
		t.Fatal("update --check modified the lockfile")
	}

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"update", "review", "--accept", "--catalog", catalogPath, "--json"}); code != ExitSuccess {
		t.Fatalf("update accept code = %d, stderr = %q", code, stderr.String())
	}
	var accepted updateReport
	if err := json.Unmarshal(stdout.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	if !accepted.Accepted || accepted.Summary.Changed != 1 {
		t.Fatalf("accepted update = %#v", accepted)
	}
	afterAccept, err := lockfile.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if afterAccept.Skills["review"].Source.ResolvedCommit != secondCommit {
		t.Fatalf("accepted commit = %q, want %q", afterAccept.Skills["review"].Source.ResolvedCommit, secondCommit)
	}
	approved, err := security.IsApproved("personal/review", security.KeyFor(afterAccept.Skills["review"]))
	if err != nil {
		t.Fatal(err)
	}
	if approved {
		t.Fatal("approval remained valid after accepting changed content")
	}

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"update", "review", "--check", "--catalog", catalogPath, "--json"}); code != ExitSuccess {
		t.Fatalf("repeat check code = %d, stderr = %q", code, stderr.String())
	}
	var repeated updateReport
	if err := json.Unmarshal(stdout.Bytes(), &repeated); err != nil {
		t.Fatal(err)
	}
	if repeated.Summary.Unchanged != 1 || repeated.Summary.Changed != 0 {
		t.Fatalf("repeat check = %#v", repeated)
	}
}
