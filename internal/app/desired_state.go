package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/paragon-h/agx/internal/catalog"
	"github.com/paragon-h/agx/internal/contenthash"
	"github.com/paragon-h/agx/internal/instructions"
	"github.com/paragon-h/agx/internal/lockfile"
	"github.com/paragon-h/agx/internal/mcpconfig"
)

type catalogDeploymentInput struct {
	Name     string
	Document catalog.Document
	LockPath string
	Locked   lockfile.Lockfile
}

type desiredSkill struct {
	CatalogName   string
	Name          string
	QualifiedName string
	Document      catalog.Document
	Skill         catalog.Skill
	LockedSkill   lockfile.LockedSkill
}

type desiredState struct {
	Catalogs     []catalogDeploymentInput
	Skills       []desiredSkill
	Instructions []desiredInstruction
	MCPConfigs   []desiredMCPConfig
	Profile      string
}

type desiredInstruction struct {
	Target        string
	Content       []byte
	ManagedDigest string
}

type desiredMCPConfig struct {
	Target        string
	Servers       map[string]lockfile.LockedMCPServer
	Content       []byte
	ManagedDigest string
}

type invalidProfileError struct {
	err error
}

func (e invalidProfileError) Error() string { return e.err.Error() }
func (e invalidProfileError) Unwrap() error { return e.err }

func loadDesiredState(ctx context.Context, collection catalog.Collection, profileName, explicitLockPath string) (desiredState, int, error) {
	if explicitLockPath != "" && len(collection.Names) != 1 {
		return desiredState{}, ExitInvalidConfig, fmt.Errorf("--lockfile cannot be used with multiple Catalogs")
	}
	result := desiredState{}
	inputs := make(map[string]catalogDeploymentInput, len(collection.Names))
	for _, name := range collection.Names {
		document := collection.Documents[name]
		lockPath := explicitLockPath
		if lockPath == "" {
			lockPath = filepath.Join(document.Root, "agx.lock")
		}
		locked, err := lockfile.Load(lockPath)
		if err != nil {
			return desiredState{}, ExitLockOutdated, fmt.Errorf("catalog %q lockfile: %w", name, err)
		}
		if code, err := verifyPlanSources(ctx, document, locked); err != nil {
			return desiredState{}, code, fmt.Errorf("catalog %q: %w", name, err)
		}
		input := catalogDeploymentInput{Name: name, Document: document, LockPath: lockPath, Locked: locked}
		inputs[name] = input
		result.Catalogs = append(result.Catalogs, input)
	}
	selection, err := collection.SelectProfile(profileName)
	if err != nil {
		return desiredState{}, ExitInvalidConfig, invalidProfileError{err: err}
	}
	if len(collection.Names) == 1 {
		result.Profile = profileName
	} else {
		result.Profile = selection.Profile
	}
	for _, resource := range selection.Resources {
		input := inputs[resource.CatalogName]
		lockedSkill, exists := input.Locked.Skills[resource.Name]
		if !exists {
			return desiredState{}, ExitLockOutdated, fmt.Errorf("catalog %q skill %q is missing from lockfile", resource.CatalogName, resource.Name)
		}
		result.Skills = append(result.Skills, desiredSkill{
			CatalogName:   resource.CatalogName,
			Name:          resource.Name,
			QualifiedName: resource.QualifiedName,
			Document:      resource.Document,
			Skill:         resource.Skill,
			LockedSkill:   lockedSkill,
		})
	}
	instructionFragments := make(map[string][][]byte)
	for _, resource := range selection.Instructions {
		input := inputs[resource.CatalogName]
		lockedInstruction, exists := input.Locked.Instructions[resource.Name]
		if !exists {
			return desiredState{}, ExitLockOutdated, fmt.Errorf("catalog %q instructions %q is missing from lockfile", resource.CatalogName, resource.Name)
		}
		for _, target := range enabledTargets(resource.Instruction.Targets) {
			instructionFragments[target] = append(instructionFragments[target], []byte(lockedInstruction.Content))
		}
	}
	for _, target := range sortedMapKeys(instructionFragments) {
		content, err := instructions.Compose(instructionFragments[target])
		if err != nil {
			return desiredState{}, ExitInvalidConfig, fmt.Errorf("compose %s Instructions: %w", target, err)
		}
		result.Instructions = append(result.Instructions, desiredInstruction{Target: target, Content: content, ManagedDigest: contenthash.Bytes(content)})
	}
	mcpServers := make(map[string]map[string]lockfile.LockedMCPServer)
	mcpOwners := make(map[string]string)
	for _, resource := range selection.MCPServers {
		input := inputs[resource.CatalogName]
		lockedServer, exists := input.Locked.MCPServers[resource.Name]
		if !exists {
			return desiredState{}, ExitLockOutdated, fmt.Errorf("catalog %q MCP server %q is missing from lockfile", resource.CatalogName, resource.Name)
		}
		for _, target := range enabledTargets(resource.MCPServer.Targets) {
			key := target + "\x00" + resource.Name
			if owner, exists := mcpOwners[key]; exists {
				return desiredState{}, ExitInvalidConfig, fmt.Errorf("MCP servers %s and %s use the same %s server name %q", owner, resource.QualifiedName, target, resource.Name)
			}
			mcpOwners[key] = resource.QualifiedName
			if mcpServers[target] == nil {
				mcpServers[target] = make(map[string]lockfile.LockedMCPServer)
			}
			mcpServers[target][resource.Name] = lockedServer
		}
	}
	for _, target := range sortedMapKeys(mcpServers) {
		servers := make(map[string]mcpconfig.Server, len(mcpServers[target]))
		for name, server := range mcpServers[target] {
			envVars := make([]string, 0, len(server.Environment))
			for variable := range server.Environment {
				envVars = append(envVars, variable)
			}
			servers[name] = mcpconfig.Server{Executable: server.Command.Executable, Args: append([]string(nil), server.Command.Args...), EnvVars: envVars}
		}
		content, err := mcpconfig.Compose(servers)
		if err != nil {
			return desiredState{}, ExitInvalidConfig, fmt.Errorf("compose %s MCP servers: %w", target, err)
		}
		result.MCPConfigs = append(result.MCPConfigs, desiredMCPConfig{Target: target, Servers: mcpServers[target], Content: content, ManagedDigest: contenthash.Bytes(content)})
	}
	return result, ExitSuccess, nil
}

func desiredStateErrorCode(exitCode int, err error) string {
	var profileError invalidProfileError
	if errors.As(err, &profileError) {
		return "AGX_PROFILE_INVALID"
	}
	return planErrorCode(exitCode)
}

func (d desiredState) catalogNames() []string {
	names := make([]string, 0, len(d.Catalogs))
	for _, input := range d.Catalogs {
		names = append(names, input.Name)
	}
	return names
}

func (d desiredState) targetNames() []string {
	seen := make(map[string]struct{})
	for _, skill := range d.Skills {
		for _, target := range enabledTargets(skill.Skill.Targets) {
			seen[target] = struct{}{}
		}
	}
	for _, instruction := range d.Instructions {
		seen[instruction.Target] = struct{}{}
	}
	for _, config := range d.MCPConfigs {
		seen[config.Target] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (d desiredState) skillTargetNames() map[string]struct{} {
	seen := make(map[string]struct{})
	for _, skill := range d.Skills {
		for _, target := range enabledTargets(skill.Skill.Targets) {
			seen[target] = struct{}{}
		}
	}
	return seen
}

func sortedMapKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (d desiredState) skillByQualifiedName(name string) (desiredSkill, bool) {
	for _, skill := range d.Skills {
		if skill.QualifiedName == name {
			return skill, true
		}
	}
	return desiredSkill{}, false
}

func (d desiredState) catalogDigest() string {
	values := make([]string, 0, len(d.Catalogs))
	for _, input := range d.Catalogs {
		values = append(values, input.Name+"="+input.Locked.CatalogDigest)
	}
	return aggregateDigest(values)
}

func (d desiredState) lockfileDigest() (string, error) {
	values := make([]string, 0, len(d.Catalogs))
	for _, input := range d.Catalogs {
		digest, err := contenthash.File(input.LockPath)
		if err != nil {
			return "", err
		}
		values = append(values, input.Name+"="+digest)
	}
	return aggregateDigest(values), nil
}

func aggregateDigest(values []string) string {
	if len(values) == 1 {
		if _, digest, found := strings.Cut(values[0], "="); found {
			return digest
		}
	}
	sort.Strings(values)
	hash := sha256.New()
	for _, value := range values {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{'\n'})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}
