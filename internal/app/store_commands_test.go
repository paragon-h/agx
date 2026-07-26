package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paragon-h/agx/internal/contenthash"
	contentstore "github.com/paragon-h/agx/internal/store"
)

func TestRunnerStoreStatusVerifyAndGC(t *testing.T) {
	t.Setenv("AGX_STORE_HOME", t.TempDir())
	root := t.TempDir()
	keptSource := writeStoreCommandFixture(t, root, "kept")
	garbageSource := writeStoreCommandFixture(t, root, "garbage")
	keptDigest, err := contenthash.Directory(keptSource)
	if err != nil {
		t.Fatal(err)
	}
	garbageDigest, err := contenthash.Directory(garbageSource)
	if err != nil {
		t.Fatal(err)
	}
	if err := contentstore.Put(keptSource, keptDigest); err != nil {
		t.Fatal(err)
	}
	if err := contentstore.Put(garbageSource, garbageDigest); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(root, "agx.lock")
	if err := contentstore.SaveReference(lockPath, []string{keptDigest}); err != nil {
		t.Fatal(err)
	}
	runner, stdout, stderr := newStoreRunner()
	if code := runner.Run(context.Background(), []string{"store", "status", "--json"}); code != ExitSuccess {
		t.Fatalf("store status code = %d, stderr = %q", code, stderr.String())
	}
	var report storeReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Summary.Objects != 2 || report.Summary.Referenced != 1 || report.Summary.Unreferenced != 1 {
		t.Fatalf("store status = %#v", report)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"store", "verify"}); code != ExitSuccess {
		t.Fatalf("store verify code = %d, stderr = %q, stdout = %q", code, stderr.String(), stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"store", "gc", "--dry-run"}); code != ExitSuccess {
		t.Fatalf("store dry-run code = %d, stderr = %q", code, stderr.String())
	}
	if _, err := contentstore.ObjectPath(garbageDigest); err != nil {
		t.Fatal(err)
	} else if err := contentstore.Verify(garbageDigest); err != nil {
		t.Fatalf("dry-run removed garbage: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"store", "gc"}); code != ExitSuccess {
		t.Fatalf("store gc code = %d, stderr = %q", code, stderr.String())
	}
	if err := contentstore.Verify(garbageDigest); err == nil {
		t.Fatal("gc left unreferenced object")
	}
	if err := contentstore.Verify(keptDigest); err != nil {
		t.Fatalf("gc removed referenced object: %v", err)
	}
}

func TestRunnerStoreGCPruneStaleReference(t *testing.T) {
	t.Setenv("AGX_STORE_HOME", t.TempDir())
	source := writeStoreCommandFixture(t, t.TempDir(), "stale")
	digest, err := contenthash.Directory(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := contentstore.Put(source, digest); err != nil {
		t.Fatal(err)
	}
	missingLock := filepath.Join(t.TempDir(), "missing", "agx.lock")
	if err := contentstore.SaveReference(missingLock, []string{digest}); err != nil {
		t.Fatal(err)
	}
	runner, stdout, stderr := newStoreRunner()
	if code := runner.Run(context.Background(), []string{"store", "gc"}); code != ExitSuccess {
		t.Fatalf("default gc code = %d, stderr = %q", code, stderr.String())
	}
	if err := contentstore.Verify(digest); err != nil {
		t.Fatalf("default gc removed stale referenced object: %v", err)
	}
	if references, err := contentstore.References(); err != nil {
		t.Fatal(err)
	} else if len(references) != 1 {
		t.Fatalf("default gc changed stale references = %#v", references)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"store", "gc", "--dry-run", "--prune-stale", "--json"}); code != ExitSuccess {
		t.Fatalf("dry-run prune code = %d, stderr = %q", code, stderr.String())
	}
	if len(stdout.Bytes()) == 0 {
		t.Fatal("dry-run prune returned empty JSON")
	}
	var dryRun storeGCReport
	if err := json.Unmarshal(stdout.Bytes(), &dryRun); err != nil {
		t.Fatal(err)
	}
	if dryRun.StaleCandidates != 1 || len(dryRun.Candidates) != 1 || dryRun.Candidates[0] != digest {
		t.Fatalf("dry-run prune report = %#v", dryRun)
	}
	if err := contentstore.Verify(digest); err != nil {
		t.Fatalf("dry-run prune removed object: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"store", "gc", "--prune-stale"}); code != ExitFailure {
		t.Fatalf("unforced prune code = %d, want %d; stderr = %q", code, ExitFailure, stderr.String())
	}
	if references, err := contentstore.References(); err != nil {
		t.Fatal(err)
	} else if len(references) != 1 {
		t.Fatalf("unforced prune changed references = %#v", references)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"store", "gc", "--prune-stale", "--force"}); code != ExitSuccess {
		t.Fatalf("prune code = %d, stderr = %q", code, stderr.String())
	}
	if err := contentstore.Verify(digest); err == nil {
		t.Fatal("prune stale did not make object collectible")
	}
	if references, err := contentstore.References(); err != nil {
		t.Fatal(err)
	} else if len(references) != 0 {
		t.Fatalf("references after prune = %#v", references)
	}
}

func TestRunnerStoreGCRequiresForceWithoutReferences(t *testing.T) {
	t.Setenv("AGX_STORE_HOME", t.TempDir())
	source := writeStoreCommandFixture(t, t.TempDir(), "unrooted")
	digest, err := contenthash.Directory(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := contentstore.Put(source, digest); err != nil {
		t.Fatal(err)
	}
	runner, stdout, stderr := newStoreRunner()
	if code := runner.Run(context.Background(), []string{"store", "gc"}); code != ExitFailure {
		t.Fatalf("unforced gc code = %d, want %d; stderr = %q", code, ExitFailure, stderr.String())
	}
	if err := contentstore.Verify(digest); err != nil {
		t.Fatalf("unforced gc removed object: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"store", "gc", "--force"}); code != ExitSuccess {
		t.Fatalf("forced gc code = %d, stderr = %q", code, stderr.String())
	}
	if err := contentstore.Verify(digest); err == nil {
		t.Fatal("forced gc left unreferenced object")
	}
}

func TestRunnerStoreRejectsCorruptReferencedObject(t *testing.T) {
	t.Setenv("AGX_STORE_HOME", t.TempDir())
	source := writeStoreCommandFixture(t, t.TempDir(), "corrupt")
	digest, err := contenthash.Directory(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := contentstore.Put(source, digest); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(t.TempDir(), "agx.lock")
	if err := contentstore.SaveReference(lockPath, []string{digest}); err != nil {
		t.Fatal(err)
	}
	objectPath, err := contentstore.ObjectPath(digest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(objectPath, "SKILL.md"), []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner, _, stderr := newStoreRunner()
	if code := runner.Run(context.Background(), []string{"store", "gc", "--prune-stale", "--force"}); code != ExitFailure {
		t.Fatalf("corrupt gc code = %d, want %d; stderr = %q", code, ExitFailure, stderr.String())
	}
	if !strings.Contains(stderr.String(), "AGX_STORE_INVALID") {
		t.Fatalf("corrupt gc stderr = %q", stderr.String())
	}
	if references, err := contentstore.References(); err != nil {
		t.Fatal(err)
	} else if len(references) != 1 {
		t.Fatalf("corrupt gc pruned references = %#v", references)
	}
}

func newStoreRunner() (*Runner, *bytes.Buffer, *bytes.Buffer) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	return New(stdout, stderr, "dev"), stdout, stderr
}

func writeStoreCommandFixture(t *testing.T, root, name string) string {
	t.Helper()
	directory := filepath.Join(root, name)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte(name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return directory
}
