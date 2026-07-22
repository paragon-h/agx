package gitresolver

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paragon-h/agx/internal/contenthash"
)

func TestResolveSkillBranchTagAndCommit(t *testing.T) {
	repository, firstCommit, secondCommit := createRepositoryFixture(t)
	resolver := New()

	tagResult, err := resolver.ResolveSkill(context.Background(), Request{
		Repository: repository,
		Revision:   "v1.0.0",
		Path:       "skills/code-review",
	})
	if err != nil {
		t.Fatal(err)
	}
	if tagResult.ResolvedCommit != firstCommit {
		t.Fatalf("tag resolved commit = %q, want %q", tagResult.ResolvedCommit, firstCommit)
	}
	wantDigest, err := contenthash.Directory(filepath.Join(repository, "skills", "code-review"))
	if err != nil {
		t.Fatal(err)
	}
	if tagResult.ContentDigest != wantDigest {
		t.Fatalf("resolved digest = %q, want working tree digest %q", tagResult.ContentDigest, wantDigest)
	}

	branchResult, err := resolver.ResolveSkill(context.Background(), Request{
		Repository: repository,
		Revision:   "main",
		Path:       "skills/code-review",
	})
	if err != nil {
		t.Fatal(err)
	}
	if branchResult.ResolvedCommit != secondCommit {
		t.Fatalf("branch resolved commit = %q, want %q", branchResult.ResolvedCommit, secondCommit)
	}
	if branchResult.ContentDigest != tagResult.ContentDigest {
		t.Fatalf("unrelated repository change altered selected Skill digest: %q != %q", branchResult.ContentDigest, tagResult.ContentDigest)
	}

	commitResult, err := resolver.ResolveSkill(context.Background(), Request{
		Repository: repository,
		Revision:   firstCommit,
		Path:       "skills/code-review",
	})
	if err != nil {
		t.Fatal(err)
	}
	if commitResult.ResolvedCommit != firstCommit || commitResult.ContentDigest != tagResult.ContentDigest {
		t.Fatalf("commit result = %#v, want commit %q and digest %q", commitResult, firstCommit, tagResult.ContentDigest)
	}
}

func TestResolveSkillRejectsMissingManifest(t *testing.T) {
	repository := t.TempDir()
	runGit(t, repository, "init", "--quiet", "--initial-branch=main")
	runGit(t, repository, "config", "user.name", "AGX Test")
	runGit(t, repository, "config", "user.email", "agx@example.invalid")
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("no skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", "README.md")
	runGit(t, repository, "commit", "--quiet", "-m", "initial")

	_, err := New().ResolveSkill(context.Background(), Request{Repository: repository, Revision: "main"})
	if err == nil || !strings.Contains(err.Error(), "SKILL.md") {
		t.Fatalf("ResolveSkill() error = %v, want missing SKILL.md error", err)
	}
}

func TestResolveSkillRejectsSubmodule(t *testing.T) {
	repository := t.TempDir()
	runGit(t, repository, "init", "--quiet", "--initial-branch=main")
	runGit(t, repository, "config", "user.name", "AGX Test")
	runGit(t, repository, "config", "user.email", "agx@example.invalid")
	if err := os.WriteFile(filepath.Join(repository, "SKILL.md"), []byte("# Root skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", "SKILL.md")
	runGit(t, repository, "commit", "--quiet", "-m", "initial")
	commit := strings.TrimSpace(runGit(t, repository, "rev-parse", "HEAD"))
	runGit(t, repository, "update-index", "--add", "--cacheinfo", "160000", commit, "vendor/dependency")
	runGit(t, repository, "commit", "--quiet", "-m", "add gitlink")

	_, err := New().ResolveSkill(context.Background(), Request{Repository: repository, Revision: "main"})
	if err == nil || !strings.Contains(err.Error(), "submodules") {
		t.Fatalf("ResolveSkill() error = %v, want submodule rejection", err)
	}
}

func TestResolveSkillRejectsSymlink(t *testing.T) {
	repository := t.TempDir()
	runGit(t, repository, "init", "--quiet", "--initial-branch=main")
	runGit(t, repository, "config", "user.name", "AGX Test")
	runGit(t, repository, "config", "user.email", "agx@example.invalid")
	if err := os.WriteFile(filepath.Join(repository, "SKILL.md"), []byte("# Root skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("SKILL.md", filepath.Join(repository, "linked-skill")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	runGit(t, repository, "add", ".")
	runGit(t, repository, "commit", "--quiet", "-m", "add symlink")

	_, err := New().ResolveSkill(context.Background(), Request{Repository: repository, Revision: "main"})
	if err == nil || !strings.Contains(err.Error(), "symbolic links") {
		t.Fatalf("ResolveSkill() error = %v, want symlink rejection", err)
	}
}

func createRepositoryFixture(t *testing.T) (string, string, string) {
	t.Helper()
	repository := t.TempDir()
	runGit(t, repository, "init", "--quiet", "--initial-branch=main")
	runGit(t, repository, "config", "user.name", "AGX Test")
	runGit(t, repository, "config", "user.email", "agx@example.invalid")
	skillRoot := filepath.Join(repository, "skills", "code-review")
	if err := os.MkdirAll(skillRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte("# Code review\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "check.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", ".")
	runGit(t, repository, "commit", "--quiet", "-m", "add skill")
	firstCommit := strings.TrimSpace(runGit(t, repository, "rev-parse", "HEAD"))
	runGit(t, repository, "tag", "v1.0.0")

	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("unrelated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", "README.md")
	runGit(t, repository, "commit", "--quiet", "-m", "update repository readme")
	secondCommit := strings.TrimSpace(runGit(t, repository, "rev-parse", "HEAD"))
	return repository, firstCommit, secondCommit
}

func runGit(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return string(output)
}
