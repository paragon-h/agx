package installer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/paragon-h/agx/internal/contenthash"
)

func RepairJournal(journal *Journal) error {
	if journal == nil {
		return nil
	}
	if err := journal.Validate(); err != nil {
		return err
	}
	if journal.State == StateCommitted {
		if err := verifyCommittedTargets(*journal); err != nil {
			journal.State = StateRepairRequired
			_ = SaveJournal(*journal)
			return err
		}
		if err := cleanupTransactionDirectories(*journal); err != nil {
			journal.State = StateRepairRequired
			_ = SaveJournal(*journal)
			return err
		}
		return DeleteJournal()
	}
	journal.State = StateRollingBack
	if err := SaveJournal(*journal); err != nil {
		return err
	}
	for i := len(journal.Targets) - 1; i >= 0; i-- {
		if err := repairTarget(&journal.Targets[i]); err != nil {
			journal.State = StateRepairRequired
			_ = SaveJournal(*journal)
			return fmt.Errorf("repair %s: %w", journal.Targets[i].TargetPath, err)
		}
		if err := SaveJournal(*journal); err != nil {
			return err
		}
	}
	if err := cleanupTransactionDirectories(*journal); err != nil {
		journal.State = StateRepairRequired
		_ = SaveJournal(*journal)
		return err
	}
	return DeleteJournal()
}

func repairTarget(target *TargetChange) error {
	switch target.Action {
	case "add":
		exists, digest, err := targetDigest(target.TargetPath, target.Kind)
		if err != nil {
			return err
		}
		if exists {
			if target.DesiredDigest == "" || digest != target.DesiredDigest {
				return fmt.Errorf("installed target does not match the transaction digest")
			}
			if err := removeTarget(target.TargetPath, target.Kind); err != nil {
				return err
			}
			target.Switched = true
		}
		if target.Switched {
			target.Restored = true
		}
		return nil
	case "update":
		return repairUpdatedTarget(target)
	case "remove":
		return repairRemovedTarget(target)
	default:
		return fmt.Errorf("unsupported action %q", target.Action)
	}
}

func repairUpdatedTarget(target *TargetChange) error {
	backupExists, backupDigest, err := targetDigest(target.BackupPath, target.Kind)
	if err != nil {
		return err
	}
	targetExists, targetDigestValue, err := targetDigest(target.TargetPath, target.Kind)
	if err != nil {
		return err
	}
	if backupExists {
		if target.CurrentDigest == "" || backupDigest != target.CurrentDigest {
			return fmt.Errorf("backup does not match the pre-transaction digest")
		}
		if targetExists {
			if target.DesiredDigest == "" || targetDigestValue != target.DesiredDigest {
				return fmt.Errorf("installed target does not match the transaction digest")
			}
			if err := removeTarget(target.TargetPath, target.Kind); err != nil {
				return err
			}
		}
		if err := os.Rename(target.BackupPath, target.TargetPath); err != nil {
			return err
		}
		target.Switched = true
		target.Restored = true
		return nil
	}
	if !targetExists {
		return fmt.Errorf("both target and backup are missing")
	}
	if target.CurrentDigest == "" || targetDigestValue != target.CurrentDigest {
		return fmt.Errorf("target is not in the pre-transaction state and no backup exists")
	}
	if target.Switched {
		target.Restored = true
	}
	return nil
}

func repairRemovedTarget(target *TargetChange) error {
	backupExists, backupDigest, err := targetDigest(target.BackupPath, target.Kind)
	if err != nil {
		return err
	}
	targetExists, targetDigestValue, err := targetDigest(target.TargetPath, target.Kind)
	if err != nil {
		return err
	}
	if backupExists {
		if target.CurrentDigest == "" || backupDigest != target.CurrentDigest {
			return fmt.Errorf("backup does not match the pre-transaction digest")
		}
		if targetExists {
			return fmt.Errorf("both target and backup exist")
		}
		if err := os.Rename(target.BackupPath, target.TargetPath); err != nil {
			return err
		}
		target.Switched = true
		target.Restored = true
		return nil
	}
	if !targetExists {
		return fmt.Errorf("both target and backup are missing")
	}
	if target.CurrentDigest == "" || targetDigestValue != target.CurrentDigest {
		return fmt.Errorf("target is not in the pre-transaction state and no backup exists")
	}
	if target.Switched {
		target.Restored = true
	}
	return nil
}

func verifyCommittedTargets(journal Journal) error {
	for _, target := range journal.Targets {
		exists, digest, err := targetDigest(target.TargetPath, target.Kind)
		if err != nil {
			return err
		}
		switch target.Action {
		case "add", "update":
			if !exists || target.DesiredDigest == "" || digest != target.DesiredDigest {
				return fmt.Errorf("committed target %s does not match the transaction", target.TargetPath)
			}
		case "remove":
			if exists {
				return fmt.Errorf("committed removal target %s still exists", target.TargetPath)
			}
		}
	}
	return nil
}

func targetDigest(path, kind string) (bool, string, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	switch kind {
	case "", "directory":
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return true, "", fmt.Errorf("%s is not a regular directory", path)
		}
	case "file":
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return true, "", fmt.Errorf("%s is not a regular file", path)
		}
	default:
		return true, "", fmt.Errorf("unsupported target kind %q", kind)
	}
	digest, err := contenthash.Path(path, kind)
	return true, digest, err
}

func removeTarget(path, kind string) error {
	if kind == "file" {
		return os.Remove(path)
	}
	return os.RemoveAll(path)
}

func cleanupTransactionDirectories(journal Journal) error {
	var cleanupErrors []string
	for _, target := range journal.Targets {
		if target.StagePath != "" {
			if err := os.RemoveAll(filepath.Dir(target.StagePath)); err != nil {
				cleanupErrors = append(cleanupErrors, err.Error())
			}
		}
		if target.BackupPath != "" {
			if err := os.RemoveAll(filepath.Dir(target.BackupPath)); err != nil {
				cleanupErrors = append(cleanupErrors, err.Error())
			}
		}
	}
	if len(cleanupErrors) > 0 {
		return errors.New(strings.Join(cleanupErrors, "; "))
	}
	return nil
}
