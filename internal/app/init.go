package app

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/alanhuangch/agx/internal/catalog"
)

const initCatalogTemplate = `apiVersion: agx.dev/v1alpha1
kind: Catalog
metadata:
  name: %s
skills: {}
`

func (r *Runner) init(args []string) int {
	if helpRequested(args) {
		fmt.Fprintln(r.stdout, "Usage: agx init [--catalog PATH] [--name NAME]")
		return ExitSuccess
	}
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	catalogPath := flags.String("catalog", "agx.yaml", "catalog path")
	name := flags.String("name", "", "catalog name (defaults to the catalog directory name)")
	if err := flags.Parse(args); err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", err)
	}
	if flags.NArg() != 0 {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", fmt.Errorf("unexpected arguments: %v", flags.Args()))
	}
	absolute, err := filepath.Abs(*catalogPath)
	if err != nil {
		return r.commandError(ExitFailure, "AGX_INIT_FAILED", err)
	}
	if _, err := os.Lstat(absolute); err == nil {
		return r.commandError(ExitFailure, "AGX_INIT_EXISTS", fmt.Errorf("catalog already exists: %s", absolute))
	} else if !os.IsNotExist(err) {
		return r.commandError(ExitFailure, "AGX_INIT_FAILED", err)
	}
	root := filepath.Dir(absolute)
	catalogName := *name
	if catalogName == "" {
		catalogName = filepath.Base(root)
		if !catalog.ValidName(catalogName) {
			catalogName = "personal"
		}
	}
	if !catalog.ValidName(catalogName) {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_CONFIG", fmt.Errorf("catalog name %q must be a lowercase resource name", catalogName))
	}
	if err := os.MkdirAll(filepath.Join(root, "skills"), 0o755); err != nil {
		return r.commandError(ExitFailure, "AGX_INIT_FAILED", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "overlays"), 0o755); err != nil {
		return r.commandError(ExitFailure, "AGX_INIT_FAILED", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "instructions"), 0o755); err != nil {
		return r.commandError(ExitFailure, "AGX_INIT_FAILED", err)
	}
	file, err := os.OpenFile(absolute, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return r.commandError(ExitFailure, "AGX_INIT_FAILED", err)
	}
	created := true
	defer func() {
		if created {
			_ = os.Remove(absolute)
		}
	}()
	if _, err := fmt.Fprintf(file, initCatalogTemplate, catalogName); err != nil {
		_ = file.Close()
		return r.commandError(ExitFailure, "AGX_INIT_FAILED", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return r.commandError(ExitFailure, "AGX_INIT_FAILED", err)
	}
	if err := file.Close(); err != nil {
		return r.commandError(ExitFailure, "AGX_INIT_FAILED", err)
	}
	created = false
	fmt.Fprintf(r.stdout, "initialized catalog %s\n", absolute)
	return ExitSuccess
}
