package installer

import "fmt"

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
	Agent      string `json:"agent"`
	Action     string `json:"action"`
	TargetPath string `json:"targetPath"`
	StagePath  string `json:"stagePath,omitempty"`
	BackupPath string `json:"backupPath,omitempty"`
	Switched   bool   `json:"switched"`
	Restored   bool   `json:"restored"`
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
		if target.Action != "add" && target.Action != "update" && target.Action != "remove" {
			return fmt.Errorf("target %d has invalid action %q", i, target.Action)
		}
		if target.Action == "update" && (target.StagePath == "" || target.BackupPath == "") {
			return fmt.Errorf("target %d update requires stage and backup paths", i)
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
