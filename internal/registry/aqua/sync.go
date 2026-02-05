package aqua

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/terassyi/toto/internal/state"
)

// Store is an interface for state storage operations.
type Store interface {
	Lock() error
	Unlock() error
	Load() (*state.UserState, error)
	Save(*state.UserState) error
}

// SyncRegistry fetches the latest aqua-registry ref and updates state if changed.
func SyncRegistry(ctx context.Context, store Store) error {
	if err := store.Lock(); err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer func() { _ = store.Unlock() }()

	currentState, err := store.Load()
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	client := NewVersionClient()
	newRef, err := client.GetLatestRef(ctx)
	if err != nil {
		return fmt.Errorf("failed to get latest aqua registry ref: %w", err)
	}

	// Check if registry needs update
	var oldRef string
	if currentState.Registry != nil && currentState.Registry.Aqua != nil {
		oldRef = currentState.Registry.Aqua.Ref
	}

	if oldRef == newRef {
		slog.Info("aqua registry is up to date", "ref", newRef)
		return nil
	}

	// Update registry state
	currentState.Registry = &state.RegistryState{
		Aqua: &state.AquaRegistryState{
			Ref:       newRef,
			UpdatedAt: time.Now(),
		},
	}

	if err := store.Save(currentState); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	slog.Info("aqua registry updated", "from", oldRef, "to", newRef)
	return nil
}
