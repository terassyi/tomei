package executor

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/terassyi/tomei/internal/installer/reconciler"
	"github.com/terassyi/tomei/internal/resource"
)

// Installer defines the interface for installing resources.
type Installer[R resource.Resource, S resource.State] interface {
	Install(ctx context.Context, res R, name string) (S, error)
	Remove(ctx context.Context, state S, name string) error
}

// StateStore defines the interface for state persistence.
type StateStore[S resource.State] interface {
	Load(name string) (S, bool, error)
	Save(name string, state S) error
	Delete(name string) error
}

// Executor executes actions for a specific resource type.
type Executor[R resource.Resource, S resource.State] struct {
	installer Installer[R, S]
	store     StateStore[S]
	kind      resource.Kind
}

// New creates a new Executor.
func New[R resource.Resource, S resource.State](
	kind resource.Kind,
	installer Installer[R, S],
	store StateStore[S],
) *Executor[R, S] {
	return &Executor[R, S]{
		installer: installer,
		store:     store,
		kind:      kind,
	}
}

// Execute executes a single action and updates the state.
func (e *Executor[R, S]) Execute(ctx context.Context, action reconciler.Action[R, S]) error { //nolint:gocritic
	slog.Debug("executing action", "type", action.Type, "kind", e.kind, "name", action.Name)

	switch action.Type {
	case resource.ActionInstall, resource.ActionUpgrade, resource.ActionReinstall:
		return e.install(ctx, action)
	case resource.ActionRemove:
		return e.remove(ctx, action)
	default:
		return fmt.Errorf("unsupported action type: %s", action.Type)
	}
}

// install installs or upgrades a resource.
func (e *Executor[R, S]) install(ctx context.Context, action reconciler.Action[R, S]) error {
	slog.Debug("installing resource", "kind", e.kind, "name", action.Name, "action", action.Type)

	// Propagate action type to installers via context
	ctx = WithAction(ctx, action.Type)

	// Install the resource
	state, err := e.installer.Install(ctx, action.Resource, action.Name)
	if err != nil {
		return fmt.Errorf("failed to install %s %s: %w", e.kind, action.Name, err)
	}

	// Update state
	if err := e.store.Save(action.Name, state); err != nil {
		return fmt.Errorf("failed to save state for %s %s: %w", e.kind, action.Name, err)
	}

	slog.Debug("resource installed successfully", "kind", e.kind, "name", action.Name)
	return nil
}

// remove removes a resource.
func (e *Executor[R, S]) remove(ctx context.Context, action reconciler.Action[R, S]) error {
	slog.Debug("removing resource", "kind", e.kind, "name", action.Name)

	// Remove the resource
	if err := e.installer.Remove(ctx, action.State, action.Name); err != nil {
		return fmt.Errorf("failed to remove %s %s: %w", e.kind, action.Name, err)
	}

	// Remove from state
	if err := e.store.Delete(action.Name); err != nil {
		return fmt.Errorf("failed to delete state for %s %s: %w", e.kind, action.Name, err)
	}

	slog.Debug("resource removed successfully", "kind", e.kind, "name", action.Name)
	return nil
}
