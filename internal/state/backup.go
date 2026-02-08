package state

import (
	"encoding/json"
	"fmt"
	"os"
)

const backupSuffix = ".bak"

// BackupPath returns the backup file path for the given state file path.
func BackupPath(statePath string) string {
	return statePath + backupSuffix
}

// CreateBackup copies the current state file to state.json.bak atomically.
// If the state file doesn't exist, does nothing (no error).
// Must be called while holding the store lock.
func CreateBackup[T State](s *Store[T]) error {
	data, err := os.ReadFile(s.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing to back up
		}
		return fmt.Errorf("failed to read state for backup: %w", err)
	}

	bakPath := BackupPath(s.statePath)
	tmpPath := bakPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write backup: %w", err)
	}

	if err := os.Rename(tmpPath, bakPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename backup: %w", err)
	}

	return nil
}

// LoadBackup reads the backup state file.
// Returns nil, nil if the backup file doesn't exist.
func LoadBackup[T State](statePath string) (*T, error) {
	bakPath := BackupPath(statePath)
	data, err := os.ReadFile(bakPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read backup: %w", err)
	}

	var st T
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("failed to parse backup: %w", err)
	}

	return &st, nil
}
