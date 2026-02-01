package engine

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/terassyi/toto/internal/installer/executor"
	"github.com/terassyi/toto/internal/installer/reconciler"
	"github.com/terassyi/toto/internal/installer/tool"
	"github.com/terassyi/toto/internal/resource"
	"github.com/terassyi/toto/internal/state"
)

// ToolInstaller defines the interface for installing tools.
type ToolInstaller interface {
	Install(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error)
	Remove(ctx context.Context, st *resource.ToolState, name string) error
	RegisterRuntime(name string, info *tool.RuntimeInfo)
	RegisterInstaller(name string, info *tool.InstallerInfo)
}

// RuntimeInstaller defines the interface for installing runtimes.
type RuntimeInstaller interface {
	Install(ctx context.Context, res *resource.Runtime, name string) (*resource.RuntimeState, error)
	Remove(ctx context.Context, st *resource.RuntimeState, name string) error
}

// Engine orchestrates the apply process.
type Engine struct {
	store             *state.Store[state.UserState]
	toolInstaller     ToolInstaller
	runtimeReconciler *reconciler.Reconciler[*resource.Runtime, *resource.RuntimeState]
	runtimeExecutor   *executor.Executor[*resource.Runtime, *resource.RuntimeState]
	toolReconciler    *reconciler.Reconciler[*resource.Tool, *resource.ToolState]
	toolExecutor      *executor.Executor[*resource.Tool, *resource.ToolState]
}

// NewEngine creates a new Engine.
func NewEngine(
	toolInstaller ToolInstaller,
	runtimeInstaller RuntimeInstaller,
	store *state.Store[state.UserState],
) *Engine {
	toolStore := executor.NewToolStateStore(store)
	runtimeStore := executor.NewRuntimeStateStore(store)
	return &Engine{
		store:             store,
		toolInstaller:     toolInstaller,
		runtimeReconciler: reconciler.NewRuntimeReconciler(),
		runtimeExecutor:   executor.New(resource.KindRuntime, runtimeInstaller, runtimeStore),
		toolReconciler:    reconciler.NewToolReconciler(),
		toolExecutor:      executor.New(resource.KindTool, toolInstaller, toolStore),
	}
}

// ToolAction is an alias for tool-specific action type.
type ToolAction = reconciler.Action[*resource.Tool, *resource.ToolState]

// RuntimeAction is an alias for runtime-specific action type.
type RuntimeAction = reconciler.Action[*resource.Runtime, *resource.RuntimeState]

// Apply reconciles resources with state and executes actions.
func (e *Engine) Apply(ctx context.Context, resources []resource.Resource) error {
	slog.Info("applying configuration", "resources", len(resources))

	// Plan first
	runtimeActions, toolActions, err := e.PlanAll(ctx, resources)
	if err != nil {
		return err
	}

	if len(runtimeActions) == 0 && len(toolActions) == 0 {
		slog.Info("no changes to apply")
		return nil
	}

	// Acquire lock for execution
	if err := e.store.Lock(); err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer func() { _ = e.store.Unlock() }()

	// Track updated runtimes for taint logic
	updatedRuntimes := make(map[string]bool)

	// Execute runtime actions first (runtimes must be installed before tools)
	for _, action := range runtimeActions {
		if e.runtimeExecutor == nil {
			return fmt.Errorf("runtime executor not configured")
		}
		if err := e.runtimeExecutor.Execute(ctx, action); err != nil {
			return fmt.Errorf("failed to execute action %s for runtime %s: %w", action.Type, action.Name, err)
		}
		// Track if runtime was updated (install, upgrade, reinstall)
		if action.Type == resource.ActionInstall || action.Type == resource.ActionUpgrade || action.Type == resource.ActionReinstall {
			updatedRuntimes[action.Name] = true
		}
	}

	// Load current state for tool installation
	st, err := e.store.Load()
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	// Register runtimes for delegation pattern
	for name, runtimeState := range st.Runtimes {
		e.toolInstaller.RegisterRuntime(name, &tool.RuntimeInfo{
			InstallPath: runtimeState.InstallPath,
			ToolBinPath: runtimeState.ToolBinPath,
			Env:         runtimeState.Env,
			Commands:    runtimeState.Commands,
		})
	}

	// Register installers for delegation pattern
	for _, res := range resources {
		if inst, ok := res.(*resource.Installer); ok && inst.InstallerSpec != nil {
			e.toolInstaller.RegisterInstaller(inst.Name(), &tool.InstallerInfo{
				Pattern:    inst.InstallerSpec.Pattern,
				RuntimeRef: inst.InstallerSpec.RuntimeRef,
				Commands:   inst.InstallerSpec.Commands,
			})
		}
	}

	// If any runtimes were updated, taint dependent tools and re-reconcile
	if len(updatedRuntimes) > 0 {
		if err := e.taintDependentTools(st, updatedRuntimes); err != nil {
			slog.Warn("failed to taint dependent tools", "error", err)
		}

		// Re-reconcile tools with updated state
		tools := extractTools(resources)
		st, err = e.store.Load()
		if err != nil {
			return fmt.Errorf("failed to reload state: %w", err)
		}
		toolActions = e.toolReconciler.Reconcile(tools, st.Tools)
	}

	// Execute tool actions
	for _, action := range toolActions {
		if err := e.toolExecutor.Execute(ctx, action); err != nil {
			return fmt.Errorf("failed to execute action %s for tool %s: %w", action.Type, action.Name, err)
		}
	}

	totalActions := len(runtimeActions) + len(toolActions)
	slog.Info("apply completed", "runtimes", len(runtimeActions), "tools", len(toolActions), "total", totalActions)
	return nil
}

// PlanAll returns both runtime and tool actions based on resources and current state.
func (e *Engine) PlanAll(ctx context.Context, resources []resource.Resource) ([]RuntimeAction, []ToolAction, error) {
	slog.Debug("planning configuration", "resources", len(resources))

	// Extract resources
	runtimes := extractRuntimes(resources)
	tools := extractTools(resources)

	// Acquire lock for state read
	if err := e.store.Lock(); err != nil {
		return nil, nil, fmt.Errorf("failed to acquire lock: %w", err)
	}

	// Load current state
	st, err := e.store.Load()
	if err != nil {
		_ = e.store.Unlock()
		return nil, nil, fmt.Errorf("failed to load state: %w", err)
	}

	_ = e.store.Unlock()

	// Reconcile runtimes
	var runtimeActions []RuntimeAction
	if e.runtimeReconciler != nil {
		runtimeActions = e.runtimeReconciler.Reconcile(runtimes, st.Runtimes)
	}

	// Reconcile tools
	toolActions := e.toolReconciler.Reconcile(tools, st.Tools)

	slog.Debug("plan completed", "runtimeActions", len(runtimeActions), "toolActions", len(toolActions))
	return runtimeActions, toolActions, nil
}

// taintDependentTools marks tools that depend on the updated runtimes for reinstallation.
// Note: Must be called while holding the store lock.
func (e *Engine) taintDependentTools(st *state.UserState, updatedRuntimes map[string]bool) error {
	taintedCount := 0
	for name, toolState := range st.Tools {
		if toolState.RuntimeRef != "" && updatedRuntimes[toolState.RuntimeRef] {
			toolState.Taint("runtime_upgraded")
			taintedCount++
			slog.Debug("tainted tool due to runtime upgrade", "tool", name, "runtime", toolState.RuntimeRef)
		}
	}

	if taintedCount > 0 {
		if err := e.store.Save(st); err != nil {
			return fmt.Errorf("failed to save tainted state: %w", err)
		}
		slog.Info("tainted tools for reinstallation", "count", taintedCount)
	}

	return nil
}

// extractRuntimes filters Runtime resources from a list of resources.
func extractRuntimes(resources []resource.Resource) []*resource.Runtime {
	var runtimes []*resource.Runtime
	for _, res := range resources {
		if rt, ok := res.(*resource.Runtime); ok {
			runtimes = append(runtimes, rt)
		}
	}
	return runtimes
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
