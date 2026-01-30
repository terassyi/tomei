package engine

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/terassyi/toto/internal/config"
	"github.com/terassyi/toto/internal/installer/executor"
	"github.com/terassyi/toto/internal/installer/reconciler"
	"github.com/terassyi/toto/internal/resource"
	"github.com/terassyi/toto/internal/state"
)

// ToolInstaller defines the interface for installing tools.
type ToolInstaller interface {
	Install(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error)
	Remove(ctx context.Context, st *resource.ToolState, name string) error
}

// Engine orchestrates the apply process.
type Engine struct {
	store          *state.Store[state.UserState]
	toolReconciler *reconciler.Reconciler[*resource.Tool, *resource.ToolState]
	toolExecutor   *executor.Executor[*resource.Tool, *resource.ToolState]
	loader         *config.Loader
}

// NewEngine creates a new Engine.
func NewEngine(toolInstaller ToolInstaller, store *state.Store[state.UserState]) *Engine {
	toolStore := executor.NewToolStateStore(store)
	return &Engine{
		store:          store,
		toolReconciler: reconciler.NewToolReconciler(),
		toolExecutor:   executor.New(resource.KindTool, toolInstaller, toolStore),
		loader:         config.NewLoader(nil),
	}
}

// ToolAction is an alias for tool-specific action type.
type ToolAction = reconciler.Action[*resource.Tool, *resource.ToolState]

// Apply loads config, reconciles with state, and executes actions.
func (e *Engine) Apply(ctx context.Context, configDir string) error {
	slog.Info("applying configuration", "dir", configDir)

	// Plan first
	toolActions, err := e.Plan(ctx, configDir)
	if err != nil {
		return err
	}

	if len(toolActions) == 0 {
		slog.Info("no changes to apply")
		return nil
	}

	// Acquire lock for execution
	if err := e.store.Lock(); err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer e.store.Unlock()

	// Execute tool actions
	for _, action := range toolActions {
		if err := e.toolExecutor.Execute(ctx, action); err != nil {
			return fmt.Errorf("failed to execute action %s for %s: %w", action.Type, action.Name, err)
		}
	}

	slog.Info("apply completed", "actions", len(toolActions))
	return nil
}

// Plan loads config and returns the actions that would be executed.
func (e *Engine) Plan(ctx context.Context, configDir string) ([]ToolAction, error) {
	slog.Debug("planning configuration", "dir", configDir)

	// Load configuration
	resources, err := e.loader.Load(configDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Extract tools from resources
	tools := extractTools(resources)

	// Acquire lock for state read
	if err := e.store.Lock(); err != nil {
		return nil, fmt.Errorf("failed to acquire lock: %w", err)
	}

	// Load current state
	st, err := e.store.Load()
	if err != nil {
		e.store.Unlock()
		return nil, fmt.Errorf("failed to load state: %w", err)
	}

	e.store.Unlock()

	// Reconcile tools
	toolActions := e.toolReconciler.Reconcile(tools, st.Tools)

	slog.Debug("plan completed", "actions", len(toolActions))
	return toolActions, nil
}

// extractTools filters Tool resources from a list of resources.
func extractTools(resources []resource.Resource) []*resource.Tool {
	var tools []*resource.Tool
	for _, res := range resources {
		if tool, ok := res.(*resource.Tool); ok {
			tools = append(tools, tool)
		}
	}
	return tools
}
