package app

import (
	"context"
	"fmt"
	"io"
)

const (
	ExitSuccess       = 0
	ExitFailure       = 1
	ExitInvalidConfig = 2
)

type Runner struct {
	stdout  io.Writer
	stderr  io.Writer
	version string
}

func New(stdout, stderr io.Writer, version string) *Runner {
	return &Runner{stdout: stdout, stderr: stderr, version: version}
}

func (r *Runner) Run(_ context.Context, args []string) int {
	if len(args) == 0 {
		r.writeHelp(r.stdout)
		return ExitSuccess
	}

	switch args[0] {
	case "help", "-h", "--help":
		r.writeHelp(r.stdout)
		return ExitSuccess
	case "version", "--version":
		fmt.Fprintf(r.stdout, "agx %s\n", r.version)
		return ExitSuccess
	case "list", "lock", "plan", "apply", "status", "doctor":
		fmt.Fprintf(r.stderr, "AGX_NOT_IMPLEMENTED: %q is part of Milestone 1 but is not implemented yet\n", args[0])
		return ExitFailure
	default:
		fmt.Fprintf(r.stderr, "AGX_UNKNOWN_COMMAND: unknown command %q\n", args[0])
		fmt.Fprintln(r.stderr, "Run 'agx help' to see available commands.")
		return ExitInvalidConfig
	}
}

func (r *Runner) writeHelp(w io.Writer) {
	fmt.Fprintln(w, `AGX manages global extensions for AI coding agents.

Usage:
  agx <command>

Commands:
  list      List skills in the active catalog
  lock      Resolve sources and write or verify the lockfile
  plan      Preview changes to agent global directories
  apply     Apply a previously reviewed plan
  status    Show the active installation generation
  doctor    Check configuration and agent integration
  version   Print the AGX version
  help      Show this help

Project status: early implementation; Milestone 1 commands may be incomplete.`)
}
