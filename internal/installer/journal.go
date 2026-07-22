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
	ID      string
	State   TransactionState
	Targets []TargetChange
}

type TargetChange struct {
	Agent      string
	TargetPath string
	BackupPath string
	Switched   bool
	Restored   bool
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
		if target.Restored && !target.Switched {
			return fmt.Errorf("target %d cannot be restored before it was switched", i)
		}
	}
	return nil
}
