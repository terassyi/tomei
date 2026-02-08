package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gofrs/flock"
)

// Store handles state file persistence with file locking.
// T must be either UserState or SystemState.
type Store[T State] struct {
	statePath string
	lockPath  string
	fileLock  *flock.Flock
	locked    bool
}

// NewUserStore creates a Store for user state.
// Default path: ~/.local/share/tomei/state.json
func NewUserStore() (*Store[UserState], error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	dir := filepath.Join(home, ".local", "share", "tomei")
	return NewStore[UserState](dir)
}

// NewSystemStore creates a Store for system state.
// Default path: /var/lib/tomei/state.json
func NewSystemStore() (*Store[SystemState], error) {
	return NewStore[SystemState]("/var/lib/tomei")
}

// NewStore creates a new Store with the given directory.
func NewStore[T State](dir string) (*Store[T], error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create state directory: %w", err)
	}

	statePath := filepath.Join(dir, "state.json")
	lockPath := filepath.Join(dir, "state.lock")

	return &Store[T]{
		statePath: statePath,
		lockPath:  lockPath,
		fileLock:  flock.New(lockPath),
		locked:    false,
	}, nil
}

// Lock acquires an exclusive lock on the state file.
// It writes the current PID to the lock file on success.
// Returns an error if another process holds the lock.
func (s *Store[T]) Lock() error {
	if s.locked {
		return nil
	}

	locked, err := s.fileLock.TryLock()
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	if !locked {
		// Read PID from lock file for error message
		pid, _ := s.readLockPID()
		if pid > 0 {
			return fmt.Errorf("another tomei process (PID %d) is running", pid)
		}
		return errors.New("another tomei process is running")
	}

	// Write our PID to the lock file
	if err := s.writeLockPID(); err != nil {
		_ = s.fileLock.Unlock()
		return fmt.Errorf("failed to write PID to lock file: %w", err)
	}

	s.locked = true
	return nil
}

// Unlock releases the lock.
func (s *Store[T]) Unlock() error {
	if !s.locked {
		return nil
	}

	if err := s.fileLock.Unlock(); err != nil {
		return fmt.Errorf("failed to release lock: %w", err)
	}

	s.locked = false
	return nil
}

// Load reads the state from disk.
// Returns a new empty state if the file doesn't exist.
// Must be called after Lock().
func (s *Store[T]) Load() (*T, error) {
	if !s.locked {
		return nil, errors.New("must acquire lock before loading state")
	}

	st, err := s.readState()
	if err != nil {
		return nil, err
	}

	s.validate(st)

	return st, nil
}

// validate runs type-specific validation on the loaded state and logs warnings.
func (s *Store[T]) validate(st *T) {
	var result *ValidationResult
	switch v := any(st).(type) {
	case *UserState:
		result = ValidateUserState(v)
	case *SystemState:
		result = ValidateSystemState(v)
	}
	if result == nil {
		return
	}
	for _, w := range result.Warnings {
		slog.Warn("state validation warning", "field", w.Field, "message", w.Message)
	}
}

// Save writes the state to disk atomically.
// Must be called after Lock().
func (s *Store[T]) Save(state *T) error {
	if !s.locked {
		return errors.New("must acquire lock before saving state")
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Write to temp file first
	tmpPath := s.statePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp state file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, s.statePath); err != nil {
		os.Remove(tmpPath) // Clean up on failure
		return fmt.Errorf("failed to rename state file: %w", err)
	}

	return nil
}

// LoadReadOnly reads the state from disk without requiring a lock.
// Use this for read-only operations like diff and plan.
func (s *Store[T]) LoadReadOnly() (*T, error) {
	return s.readState()
}

// readState reads and unmarshals the state file.
// Returns a new empty state if the file doesn't exist.
func (s *Store[T]) readState() (*T, error) {
	data, err := os.ReadFile(s.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return new(T), nil
		}
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var state T
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}

	return &state, nil
}

// StatePath returns the path to the state file.
func (s *Store[T]) StatePath() string {
	return s.statePath
}

// LockPath returns the path to the lock file.
func (s *Store[T]) LockPath() string {
	return s.lockPath
}

func (s *Store[T]) readLockPID() (int, error) {
	data, err := os.ReadFile(s.lockPath)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return 0, err
	}
	return pid, nil
}

func (s *Store[T]) writeLockPID() error {
	pid := os.Getpid()
	return os.WriteFile(s.lockPath, []byte(strconv.Itoa(pid)), 0644)
}
