package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/paragon-h/agx/internal/catalog"
	"github.com/paragon-h/agx/internal/lockfile"
)

func TestRunnerVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	runner := New(&stdout, &stderr, "v0.1.0-test")

	if code := runner.Run(context.Background(), []string{"version"}); code != ExitSuccess {
		t.Fatalf("Run() code = %d, want %d", code, ExitSuccess)
	}
	if got, want := stdout.String(), "agx v0.1.0-test\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunnerListAndLockLocalCatalog(t *testing.T) {
	root := writeLocalCatalogFixture(t)
	catalogPath := filepath.Join(root, "agx.yaml")
	lockPath := filepath.Join(root, "agx.lock")
	var stdout, stderr bytes.Buffer
	runner := New(&stdout, &stderr, "dev")

	if code := runner.Run(context.Background(), []string{"list", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("list code = %d, stderr = %q", code, stderr.String())
	}
	if got, want := stdout.String(), "personal/code-review\tlocal\tclaude,codex\n"; got != want {
		t.Fatalf("list stdout = %q, want %q", got, want)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("lock code = %d, stderr = %q", code, stderr.String())
	}
	locked, err := lockfile.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := locked.Skills["code-review"].ContentDigest; !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("content digest = %q, want sha256 digest", got)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath, "--frozen"}); code != ExitSuccess {
		t.Fatalf("frozen lock code = %d, stderr = %q", code, stderr.String())
	}

	if err := os.WriteFile(filepath.Join(root, "skills", "code-review", "SKILL.md"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath, "--frozen"}); code != ExitLockOutdated {
		t.Fatalf("changed frozen lock code = %d, want %d; stderr = %q", code, ExitLockOutdated, stderr.String())
	}
	if !strings.Contains(stderr.String(), "LOCK_OUTDATED") {
		t.Fatalf("stderr = %q, want LOCK_OUTDATED", stderr.String())
	}
}

func TestBuildLockPreservesTimestampForUnchangedSkill(t *testing.T) {
	root := writeLocalCatalogFixture(t)
	document, err := catalog.Load(filepath.Join(root, "agx.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := buildLock(context.Background(), document, time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC), nil)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := buildLock(context.Background(), document, time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC), &first)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := second.Skills["code-review"].LockedAt, first.Skills["code-review"].LockedAt; got != want {
		t.Fatalf("LockedAt = %q, want preserved %q", got, want)
	}
}

func TestRunnerLocksGitSkill(t *testing.T) {
	repository := t.TempDir()
	runGitCommand(t, repository, "init", "--quiet", "--initial-branch=main")
	runGitCommand(t, repository, "config", "user.name", "AGX Test")
	runGitCommand(t, repository, "config", "user.email", "agx@example.invalid")
	skillRoot := filepath.Join(repository, "skills", "review")
	if err := os.MkdirAll(skillRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte("# Review\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, repository, "add", ".")
	runGitCommand(t, repository, "commit", "--quiet", "-m", "add skill")
	wantCommit := strings.TrimSpace(runGitCommand(t, repository, "rev-parse", "HEAD"))

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

	var stdout, stderr bytes.Buffer
	runner := New(&stdout, &stderr, "dev")
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("lock code = %d, stderr = %q", code, stderr.String())
	}
	locked, err := lockfile.Load(filepath.Join(catalogRoot, "agx.lock"))
	if err != nil {
		t.Fatal(err)
	}
	got := locked.Skills["review"]
	if got.Source.ResolvedCommit != wantCommit {
		t.Fatalf("resolved commit = %q, want %q", got.Source.ResolvedCommit, wantCommit)
	}
	if got.Source.RequestedRevision != "main" || got.Source.Path != "skills/review" {
		t.Fatalf("locked source = %#v", got.Source)
	}
	if !strings.HasPrefix(got.ContentDigest, "sha256:") {
		t.Fatalf("content digest = %q, want sha256 digest", got.ContentDigest)
	}

	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte("# Review updated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, repository, "add", ".")
	runGitCommand(t, repository, "commit", "--quiet", "-m", "update skill")
	updatedCommit := strings.TrimSpace(runGitCommand(t, repository, "rev-parse", "HEAD"))

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath, "--frozen"}); code != ExitSuccess {
		t.Fatalf("frozen Git lock code = %d, stderr = %q", code, stderr.String())
	}
	stillLocked, err := lockfile.Load(filepath.Join(catalogRoot, "agx.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if stillLocked.Skills["review"].Source.ResolvedCommit != wantCommit {
		t.Fatal("frozen lock unexpectedly resolved the updated branch")
	}

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"lock", "--catalog", catalogPath}); code != ExitSuccess {
		t.Fatalf("updated Git lock code = %d, stderr = %q", code, stderr.String())
	}
	updatedLock, err := lockfile.Load(filepath.Join(catalogRoot, "agx.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if updatedLock.Skills["review"].Source.ResolvedCommit != updatedCommit {
		t.Fatalf("updated resolved commit = %q, want %q", updatedLock.Skills["review"].Source.ResolvedCommit, updatedCommit)
	}
	if updatedLock.Skills["review"].ContentDigest == got.ContentDigest {
		t.Fatal("updated Git Skill retained the previous content digest")
	}
}

func writeLocalCatalogFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	skillRoot := filepath.Join(root, "skills", "code-review")
	if err := os.MkdirAll(skillRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte("# Code review\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	catalogYAML := `apiVersion: agx.dev/v1alpha1
kind: Catalog
metadata:
  name: personal
skills:
  code-review:
    source:
      type: local
      path: skills/code-review
    targets:
      codex: {}
      claude: {}
`
	if err := os.WriteFile(filepath.Join(root, "agx.yaml"), []byte(catalogYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func runGitCommand(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return string(output)
}

func TestRunnerUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	runner := New(&stdout, &stderr, "dev")

	if code := runner.Run(context.Background(), []string{"nope"}); code != ExitInvalidConfig {
		t.Fatalf("Run() code = %d, want %d", code, ExitInvalidConfig)
	}
	if !strings.Contains(stderr.String(), "AGX_UNKNOWN_COMMAND") {
		t.Fatalf("stderr = %q, want stable error code", stderr.String())
	}
}
