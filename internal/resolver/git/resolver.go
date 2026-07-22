package gitresolver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/paragon-h/agx/internal/catalog"
	"github.com/paragon-h/agx/internal/contenthash"
	"github.com/paragon-h/agx/internal/filetree"
)

var (
	commitPattern        = regexp.MustCompile(`^[0-9a-f]{40}$`)
	httpsUserInfoPattern = regexp.MustCompile(`https?://[^\s/@]+@`)
)

type Resolver struct {
	Executable string
	TempRoot   string
}

type Request struct {
	Repository string
	Revision   string
	Path       string
}

type Result struct {
	ResolvedCommit string
	ContentDigest  string
}

func New() Resolver {
	return Resolver{Executable: "git"}
}

func (r Resolver) ResolveSkill(ctx context.Context, request Request) (Result, error) {
	return r.resolveSkill(ctx, request, "")
}

func (r Resolver) MaterializeSkill(ctx context.Context, request Request, destination string) (Result, error) {
	if destination == "" {
		return Result{}, errors.New("materialization destination is required")
	}
	return r.resolveSkill(ctx, request, destination)
}

func (r Resolver) resolveSkill(ctx context.Context, request Request, destination string) (Result, error) {
	if request.Repository == "" || request.Revision == "" {
		return Result{}, errors.New("repository and revision are required")
	}
	if strings.HasPrefix(request.Revision, "-") || strings.ContainsAny(request.Revision, "\r\n\x00") {
		return Result{}, errors.New("revision contains unsupported characters")
	}
	if request.Path != "" && !catalog.ValidRelativePath(request.Path) {
		return Result{}, errors.New("source path must stay within the repository root")
	}
	if r.Executable == "" {
		r.Executable = "git"
	}

	temporaryRoot, err := os.MkdirTemp(r.TempRoot, "agx-git-resolve-*")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(temporaryRoot)

	repositoryRoot := filepath.Join(temporaryRoot, "repository")
	if _, err := r.run(ctx, request.Repository, "init", "--quiet", repositoryRoot); err != nil {
		return Result{}, fmt.Errorf("initialize temporary repository: %w", err)
	}
	if _, err := r.run(ctx, request.Repository, "-C", repositoryRoot, "remote", "add", "origin", request.Repository); err != nil {
		return Result{}, fmt.Errorf("configure Git source: %w", err)
	}
	if _, err := r.run(ctx, request.Repository, "-C", repositoryRoot, "fetch", "--quiet", "--depth=1", "origin", request.Revision); err != nil {
		return Result{}, fmt.Errorf("fetch requested revision: %w", err)
	}
	commitOutput, err := r.run(ctx, request.Repository, "-C", repositoryRoot, "rev-parse", "--verify", "FETCH_HEAD^{commit}")
	if err != nil {
		return Result{}, fmt.Errorf("resolve requested revision: %w", err)
	}
	resolvedCommit := strings.TrimSpace(string(commitOutput))
	if !commitPattern.MatchString(resolvedCommit) {
		return Result{}, fmt.Errorf("resolved commit %q is not a full SHA", resolvedCommit)
	}

	exportRoot := filepath.Join(temporaryRoot, "export")
	if err := os.Mkdir(exportRoot, 0o755); err != nil {
		return Result{}, err
	}
	if err := r.exportTree(ctx, request.Repository, repositoryRoot, exportRoot, resolvedCommit, request.Path); err != nil {
		return Result{}, err
	}

	skillRoot := exportRoot
	if request.Path != "" {
		skillRoot = filepath.Join(exportRoot, filepath.FromSlash(request.Path))
	}
	manifest, err := os.Lstat(filepath.Join(skillRoot, "SKILL.md"))
	if err != nil {
		return Result{}, fmt.Errorf("resolved Skill requires SKILL.md: %w", err)
	}
	if !manifest.Mode().IsRegular() {
		return Result{}, errors.New("resolved Skill SKILL.md must be a regular file")
	}
	digest, err := contenthash.Directory(skillRoot)
	if err != nil {
		return Result{}, fmt.Errorf("hash resolved Skill: %w", err)
	}
	if destination != "" {
		if err := filetree.Copy(skillRoot, destination); err != nil {
			return Result{}, fmt.Errorf("materialize resolved Skill: %w", err)
		}
		materializedDigest, err := contenthash.Directory(destination)
		if err != nil {
			return Result{}, err
		}
		if materializedDigest != digest {
			return Result{}, errors.New("materialized Skill digest mismatch")
		}
	}
	return Result{ResolvedCommit: resolvedCommit, ContentDigest: digest}, nil
}

func (r Resolver) exportTree(ctx context.Context, repository, repositoryRoot, exportRoot, commit, sourcePath string) error {
	args := []string{"-C", repositoryRoot, "ls-tree", "-r", "-z", "--full-tree", commit}
	if sourcePath != "" {
		args = append(args, "--", sourcePath)
	}
	output, err := r.run(ctx, repository, args...)
	if err != nil {
		return fmt.Errorf("inspect resolved tree: %w", err)
	}
	for _, record := range bytes.Split(output, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		tab := bytes.IndexByte(record, '\t')
		if tab < 0 {
			return errors.New("Git tree contains an invalid record")
		}
		fields := strings.Fields(string(record[:tab]))
		if len(fields) != 3 {
			return errors.New("Git tree contains invalid metadata")
		}
		mode, objectType, object := fields[0], fields[1], fields[2]
		if mode == "160000" {
			return errors.New("Git submodules are not supported in Milestone 1")
		}
		if mode == "120000" {
			return errors.New("symbolic links are not supported in Milestone 1")
		}
		if objectType != "blob" || (mode != "100644" && mode != "100755") || !commitPattern.MatchString(object) {
			return fmt.Errorf("unsupported Git tree entry mode=%s type=%s", mode, objectType)
		}
		relative := string(record[tab+1:])
		if !catalog.ValidRelativePath(relative) {
			return fmt.Errorf("Git tree contains unsafe path %q", relative)
		}
		destination := filepath.Join(exportRoot, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		permissions := os.FileMode(0o644)
		if mode == "100755" {
			permissions = 0o755
		}
		if err := r.writeBlob(ctx, repository, repositoryRoot, object, destination, permissions); err != nil {
			return fmt.Errorf("export Git blob %s: %w", object, err)
		}
	}
	return nil
}

func (r Resolver) writeBlob(ctx context.Context, repository, repositoryRoot, object, destination string, permissions os.FileMode) error {
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, permissions)
	if err != nil {
		return err
	}
	if err := file.Chmod(permissions); err != nil {
		file.Close()
		os.Remove(destination)
		return err
	}
	command := exec.CommandContext(ctx, r.Executable, "-C", repositoryRoot, "cat-file", "blob", object)
	var stderr bytes.Buffer
	command.Stdout = file
	command.Stderr = &stderr
	runErr := command.Run()
	closeErr := file.Close()
	if runErr != nil {
		os.Remove(destination)
		return r.commandError(runErr, stderr.String(), repository)
	}
	if closeErr != nil {
		return closeErr
	}
	return nil
}

func (r Resolver) run(ctx context.Context, repository string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, r.Executable, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err == nil {
		return stdout.Bytes(), nil
	}
	return nil, r.commandError(err, stderr.String(), repository)
}

func (r Resolver) commandError(commandErr error, commandOutput, repository string) error {
	message := strings.TrimSpace(commandOutput)
	if repository != "" {
		message = strings.ReplaceAll(message, repository, "<repository>")
	}
	message = httpsUserInfoPattern.ReplaceAllString(message, "https://<redacted>@")
	if message == "" {
		return commandErr
	}
	return fmt.Errorf("%w: %s", commandErr, message)
}
