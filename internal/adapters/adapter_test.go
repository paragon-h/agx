package adapters

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveHomeRequiresAbsoluteOverride(t *testing.T) {
	t.Setenv("AGX_TEST_HOME", "relative")
	if _, err := ResolveHome("AGX_TEST_HOME", ".example"); err == nil {
		t.Fatal("ResolveHome() error = nil, want absolute path error")
	}
}

func TestResolveHomeUsesOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGX_TEST_HOME", home)
	got, err := SkillsPath("AGX_TEST_HOME", ".example")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "skills"); got != want {
		t.Fatalf("SkillsPath() = %q, want %q", got, want)
	}
}

func TestDetectExecutable(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "agx-test-agent")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	detection := DetectExecutable("agx-test-agent")
	if !detection.Installed || detection.Executable != executable {
		t.Fatalf("DetectExecutable() = %#v, want installed executable %q", detection, executable)
	}
}
