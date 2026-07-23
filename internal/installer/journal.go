package installer

import (
	"fmt"
	"path/filepath"
	"strings"
)

type TransactionState string

const (
	StatePrepared       TransactionState = "prepared"
	StateApplying       TransactionState = "applying"
	StateCommitted      TransactionState = "committed"
	StateRollingBack    TransactionState = "rolling_back"
	StateRepairRequired TransactionState = "repair_required"
)

type Journal struct {
	ID      string           `json:"id"`
	State   TransactionState `json:"state"`
	Targets []TargetChange   `json:"targets"`
}

type TargetChange struct {
	Agent         string `json:"agent"`
	Action        string `json:"action"`
	TargetPath    string `json:"targetPath"`
	StagePath     string `json:"stagePath,omitempty"`
	BackupPath    string `json:"backupPath,omitempty"`
	DesiredDigest string `json:"desiredDigest,omitempty"`
	CurrentDigest string `json:"currentDigest,omitempty"`
	Switched      bool   `json:"switched"`
	Restored      bool   `json:"restored"`
}

func (j Journal) Validate() error {
	if j.ID == "" {
		return fmt.Errorf("journal ID is required")
	}
	switch j.State {
	case StatePrepared, StateApplying, StateCommitted, StateRollingBack, StateRepairRequired:
	default:
		return fmt.Errorf("invalid transaction state %q", j.State)
	}
	for i, target := range j.Targets {
		if target.Agent == "" || target.TargetPath == "" {
			return fmt.Errorf("target %d requires agent and target path", i)
		}
		if !filepath.IsAbs(target.TargetPath) {
			return fmt.Errorf("target %d path must be absolute", i)
		}
		cleanTarget := filepath.Clean(target.TargetPath)
		if cleanTarget != target.TargetPath || cleanTarget == filepath.VolumeName(cleanTarget)+string(filepath.Separator) {
			return fmt.Errorf("target %d path is unsafe", i)
		}
		if target.Action != "add" && target.Action != "update" && target.Action != "remove" {
			return fmt.Errorf("target %d has invalid action %q", i, target.Action)
		}
		if target.Action == "update" && (target.StagePath == "" || target.BackupPath == "") {
			return fmt.Errorf("target %d update requires stage and backup paths", i)
		}
		if target.StagePath != "" && !validTemporaryContentPath(target.StagePath, ".agx-stage-", filepath.Dir(cleanTarget)) {
			return fmt.Errorf("target %d has invalid stage path", i)
		}
		if target.BackupPath != "" && !validTemporaryContentPath(target.BackupPath, ".agx-backup-", filepath.Dir(cleanTarget)) {
			return fmt.Errorf("target %d has invalid backup path", i)
		}
		if target.DesiredDigest != "" && !validDigest(target.DesiredDigest) {
			return fmt.Errorf("target %d has invalid desired digest", i)
		}
		if target.CurrentDigest != "" && !validDigest(target.CurrentDigest) {
			return fmt.Errorf("target %d has invalid current digest", i)
		}
		if target.Action == "add" && target.StagePath == "" {
			return fmt.Errorf("target %d add requires stage path", i)
		}
		if target.Action == "remove" && target.BackupPath == "" {
			return fmt.Errorf("target %d remove requires backup path", i)
		}
		if target.Restored && !target.Switched {
			return fmt.Errorf("target %d cannot be restored before it was switched", i)
		}
	}
	return nil
}

func validTemporaryContentPath(path, prefix, targetParent string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && filepath.Base(path) == "content" && strings.HasPrefix(filepath.Base(filepath.Dir(path)), prefix) && filepath.Dir(filepath.Dir(path)) == targetParent
}

func validDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, char := range value[len("sha256:"):] {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}
