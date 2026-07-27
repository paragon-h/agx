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
	"github.com/paragon-h/agx/internal/lockfile"
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
	Catalogs []catalogDeploymentInput
	Skills   []desiredSkill
	Profile  string
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
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
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
