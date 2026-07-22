package app

import (
	"bytes"
	"context"
	"strings"
	"testing"
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
