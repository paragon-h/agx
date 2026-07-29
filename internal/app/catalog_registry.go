package app

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/alanhuangch/agx/internal/catalog"
	"github.com/alanhuangch/agx/internal/registry"
)

type catalogListEntry struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Active bool   `json:"active"`
}

func (r *Runner) catalogRegistry(args []string) int {
	if len(args) == 0 || helpRequested(args) {
		r.writeCatalogHelp(r.stdout)
		return ExitSuccess
	}
	switch args[0] {
	case "add":
		return r.catalogAdd(args[1:])
	case "list":
		return r.catalogList(args[1:])
	case "use":
		return r.catalogUse(args[1:])
	case "remove":
		return r.catalogRemove(args[1:])
	default:
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", fmt.Errorf("unknown catalog command %q", args[0]))
	}
}

func (r *Runner) catalogAdd(args []string) int {
	if helpRequested(args) {
		fmt.Fprintln(r.stdout, "Usage: agx catalog add <name> --path PATH")
		return ExitSuccess
	}
	flags := flag.NewFlagSet("catalog add", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("path", "", "path to agx.yaml or its containing directory")
	normalized, err := normalizeReviewArgs(args, map[string]bool{})
	if err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", err)
	}
	if err := flags.Parse(normalized); err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", err)
	}
	if flags.NArg() != 1 || *path == "" {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", fmt.Errorf("catalog add requires one name and --path"))
	}
	name := flags.Arg(0)
	if !catalog.ValidName(name) {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", fmt.Errorf("catalog name %q is invalid", name))
	}
	resolved, err := registry.ResolveCatalogPath(*path)
	if err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_CATALOG_INVALID", err)
	}
	document, err := catalog.Load(resolved)
	if err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_CATALOG_INVALID", err)
	}
	if document.Catalog.Metadata.Name != name {
		return r.commandError(ExitInvalidConfig, "AGX_CATALOG_NAME_MISMATCH", fmt.Errorf("registered name %q differs from Catalog metadata.name %q", name, document.Catalog.Metadata.Name))
	}
	value, err := registry.Load()
	if err != nil {
		return r.commandError(ExitFailure, "AGX_CATALOG_REGISTRY_INVALID", err)
	}
	if _, exists := value.Catalogs[name]; exists {
		return r.commandError(ExitFailure, "AGX_CATALOG_EXISTS", fmt.Errorf("catalog %q is already registered", name))
	}
	for existingName, entry := range value.Catalogs {
		if sameCatalogPath(entry.Path, resolved) {
			return r.commandError(ExitFailure, "AGX_CATALOG_EXISTS", fmt.Errorf("path is already registered as %q", existingName))
		}
	}
	value.Catalogs[name] = registry.Entry{Path: resolved}
	if value.Active == "" {
		value.Active = name
	}
	if err := registry.Save(value); err != nil {
		return r.commandError(ExitFailure, "AGX_CATALOG_WRITE_FAILED", err)
	}
	fmt.Fprintf(r.stdout, "registered catalog %s -> %s", name, resolved)
	if value.Active == name {
		fmt.Fprint(r.stdout, " (active)")
	}
	fmt.Fprintln(r.stdout)
	return ExitSuccess
}

func sameCatalogPath(left, right string) bool {
	if filepath.Clean(left) == filepath.Clean(right) {
		return true
	}
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	return leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo)
}

func (r *Runner) catalogList(args []string) int {
	if helpRequested(args) {
		fmt.Fprintln(r.stdout, "Usage: agx catalog list [--json]")
		return ExitSuccess
	}
	flags := flag.NewFlagSet("catalog list", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", err)
	}
	if flags.NArg() != 0 {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", fmt.Errorf("unexpected arguments: %v", flags.Args()))
	}
	value, err := registry.Load()
	if err != nil {
		return r.commandError(ExitFailure, "AGX_CATALOG_REGISTRY_INVALID", err)
	}
	entries := make([]catalogListEntry, 0, len(value.Catalogs))
	for _, name := range registry.SortedNames(value) {
		entry := value.Catalogs[name]
		entries = append(entries, catalogListEntry{Name: name, Path: entry.Path, Active: value.Active == name})
	}
	if *jsonOutput {
		if err := json.NewEncoder(r.stdout).Encode(entries); err != nil {
			return r.commandError(ExitFailure, "AGX_OUTPUT_FAILED", err)
		}
		return ExitSuccess
	}
	for _, entry := range entries {
		marker := " "
		if entry.Active {
			marker = "*"
		}
		fmt.Fprintf(r.stdout, "%s %s\t%s\n", marker, entry.Name, entry.Path)
	}
	return ExitSuccess
}

func (r *Runner) catalogUse(args []string) int {
	if helpRequested(args) {
		fmt.Fprintln(r.stdout, "Usage: agx catalog use <name>")
		return ExitSuccess
	}
	if len(args) != 1 {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", fmt.Errorf("catalog use requires one name"))
	}
	value, err := registry.Load()
	if err != nil {
		return r.commandError(ExitFailure, "AGX_CATALOG_REGISTRY_INVALID", err)
	}
	entry, ok := value.Catalogs[args[0]]
	if !ok {
		return r.commandError(ExitInvalidConfig, "AGX_CATALOG_NOT_FOUND", fmt.Errorf("catalog %q is not registered", args[0]))
	}
	document, err := catalog.Load(entry.Path)
	if err != nil {
		return r.commandError(ExitInvalidConfig, "AGX_CATALOG_INVALID", fmt.Errorf("catalog %q is unavailable: %w", args[0], err))
	}
	if document.Catalog.Metadata.Name != args[0] {
		return r.commandError(ExitInvalidConfig, "AGX_CATALOG_NAME_MISMATCH", fmt.Errorf("registered name %q differs from Catalog metadata.name %q", args[0], document.Catalog.Metadata.Name))
	}
	value.Active = args[0]
	if err := registry.Save(value); err != nil {
		return r.commandError(ExitFailure, "AGX_CATALOG_WRITE_FAILED", err)
	}
	fmt.Fprintf(r.stdout, "active catalog: %s -> %s\n", args[0], entry.Path)
	return ExitSuccess
}

func (r *Runner) catalogRemove(args []string) int {
	if helpRequested(args) {
		fmt.Fprintln(r.stdout, "Usage: agx catalog remove <name>")
		return ExitSuccess
	}
	if len(args) != 1 {
		return r.commandError(ExitInvalidConfig, "AGX_INVALID_ARGUMENT", fmt.Errorf("catalog remove requires one name"))
	}
	value, err := registry.Load()
	if err != nil {
		return r.commandError(ExitFailure, "AGX_CATALOG_REGISTRY_INVALID", err)
	}
	entry, ok := value.Catalogs[args[0]]
	if !ok {
		return r.commandError(ExitInvalidConfig, "AGX_CATALOG_NOT_FOUND", fmt.Errorf("catalog %q is not registered", args[0]))
	}
	delete(value.Catalogs, args[0])
	if value.Active == args[0] {
		value.Active = ""
	}
	if err := registry.Save(value); err != nil {
		return r.commandError(ExitFailure, "AGX_CATALOG_WRITE_FAILED", err)
	}
	fmt.Fprintf(r.stdout, "removed catalog registration %s -> %s\n", args[0], entry.Path)
	return ExitSuccess
}

func (r *Runner) writeCatalogHelp(w io.Writer) {
	fmt.Fprintln(w, `Usage:
  agx catalog add <name> --path PATH
  agx catalog list [--json]
  agx catalog use <name>
  agx catalog remove <name>`)
}
