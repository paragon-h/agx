package security

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alanhuangch/agx/internal/state"
)

func SaveApproval(approval Approval) error {
	if err := approval.Validate(); err != nil {
		return err
	}
	path, err := approvalPath(approval.SkillQualifiedName)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(approval, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	file, err := os.CreateTemp(filepath.Dir(path), ".agx-approval-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func LoadApproval(qualifiedName string) (*Approval, error) {
	path, err := approvalPath(qualifiedName)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var approval Approval
	if err := json.Unmarshal(data, &approval); err != nil {
		return nil, fmt.Errorf("decode approval: %w", err)
	}
	if approval.SkillQualifiedName != qualifiedName {
		return nil, errors.New("approval file contains a different skill name")
	}
	if err := approval.Validate(); err != nil {
		return nil, fmt.Errorf("validate approval: %w", err)
	}
	return &approval, nil
}

func IsApproved(qualifiedName string, key ApprovalKey) (bool, error) {
	approval, err := LoadApproval(qualifiedName)
	if err != nil || approval == nil {
		return false, err
	}
	return approval.Key == key, nil
}

func approvalPath(qualifiedName string) (string, error) {
	parts := strings.Split(qualifiedName, "/")
	if !validQualifiedName(qualifiedName) {
		return "", fmt.Errorf("invalid qualified skill name %q", qualifiedName)
	}
	root, err := state.Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "approvals", parts[0], parts[1]+".json"), nil
}
