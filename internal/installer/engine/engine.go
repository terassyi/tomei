package engine

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"sync/atomic"

	"github.com/terassyi/toto/internal/graph"
	"github.com/terassyi/toto/internal/installer/download"
	"github.com/terassyi/toto/internal/installer/executor"
	"github.com/terassyi/toto/internal/installer/reconciler"
	"github.com/terassyi/toto/internal/installer/tool"
	"github.com/terassyi/toto/internal/resource"
	"github.com/terassyi/toto/internal/state"
	"golang.org/x/sync/semaphore"
)

// ToolInstaller defines the interface for installing tools.
type ToolInstaller interface {
	Install(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error)
	Remove(ctx context.Context, st *resource.ToolState, name string) error
	RegisterRuntime(name string, info *tool.RuntimeInfo)
	RegisterInstaller(name string, info *tool.InstallerInfo)
	SetProgressCallback(callback download.ProgressCallback)
	SetOutputCallback(callback download.OutputCallback)
}

// RuntimeInstaller defines the interface for installing runtimes.
type RuntimeInstaller interface {
	Install(ctx context.Context, res *resource.Runtime, name string) (*resource.RuntimeState, error)
	Remove(ctx context.Context, st *resource.RuntimeState, name string) error
	SetProgressCallback(callback download.ProgressCallback)
}

// ResolverConfigurer is a callback to configure the tool resolver after state is loaded.
// This allows resolver setup to happen after the lock is acquired and state is read.
type ResolverConfigurer func(st *state.UserState) error

// EventType represents the type of engine event.
type EventType int

const (
	// EventStart is emitted when an action starts.
	EventStart EventType = iota
	// EventProgress is emitted during download to report progress.
	EventProgress
	// EventOutput is emitted for each line of command output (delegation pattern).
	EventOutput
	// EventComplete is emitted when an action completes successfully.
	EventComplete
	// EventError is emitted when an action fails.
	EventError
)

// Event represents an engine event for progress reporting.
type Event struct {
	Type       EventType
	Kind       resource.Kind
	Name       string
	Version    string
	Action     resource.ActionType
	Error      error
	Downloaded int64  // bytes downloaded (for EventProgress)
	Total      int64  // total bytes (-1 if unknown, for EventProgress)
	Output     string // output line (for EventOutput)
	Method     string // install method: "download", "go install", etc.
}

// EventHandler is a callback for engine events.
type EventHandler func(event Event)

const (
	// DefaultParallelism is the default number of concurrent installations.
	DefaultParallelism = 5

	// MaxParallelism is the maximum allowed parallelism.
	MaxParallelism = 20
)

// Engine orchestrates the apply process.
type Engine struct {
	store              *state.Store[state.UserState]
	toolInstaller      ToolInstaller
	runtimeInstaller   RuntimeInstaller
	runtimeReconciler  *reconciler.Reconciler[*resource.Runtime, *resource.RuntimeState]
	runtimeExecutor    *executor.Executor[*resource.Runtime, *resource.RuntimeState]
	toolReconciler     *reconciler.Reconciler[*resource.Tool, *resource.ToolState]
	toolExecutor       *executor.Executor[*resource.Tool, *resource.ToolState]
	resolverConfigurer ResolverConfigurer
	eventHandler       EventHandler
	parallelism        int
}

// NewEngine creates a new Engine.
func NewEngine(
	toolInstaller ToolInstaller,
	runtimeInstaller RuntimeInstaller,
	store *state.Store[state.UserState],
) *Engine {
	storeFactory := executor.NewStateStoreFactory(store)
	toolStore := storeFactory.ToolStore()
	runtimeStore := storeFactory.RuntimeStore()
	return &Engine{
		store:             store,
		toolInstaller:     toolInstaller,
		runtimeInstaller:  runtimeInstaller,
		runtimeReconciler: reconciler.NewRuntimeReconciler(),
		runtimeExecutor:   executor.New(resource.KindRuntime, runtimeInstaller, runtimeStore),
		toolReconciler:    reconciler.NewToolReconciler(),
		toolExecutor:      executor.New(resource.KindTool, toolInstaller, toolStore),
		parallelism:       DefaultParallelism,
	}
}

// SetParallelism sets the number of concurrent installations.
// Values are clamped to [1, MaxParallelism].
func (e *Engine) SetParallelism(n int) {
	if n < 1 {
		n = 1
	}
	if n > MaxParallelism {
		n = MaxParallelism
	}
	e.parallelism = n
}

// SetResolverConfigurer sets a callback to configure the resolver after state is loaded.
// This ensures resolver configuration happens while holding the state lock.
func (e *Engine) SetResolverConfigurer(configurer ResolverConfigurer) {
	e.resolverConfigurer = configurer
}

// SetEventHandler sets a callback for engine events.
func (e *Engine) SetEventHandler(handler EventHandler) {
	e.eventHandler = handler
}

// emitEvent emits an event to the handler if set.
func (e *Engine) emitEvent(event Event) {
	if e.eventHandler != nil {
		e.eventHandler(event)
	}
}

// ToolAction is an alias for tool-specific action type.
type ToolAction = reconciler.Action[*resource.Tool, *resource.ToolState]

// RuntimeAction is an alias for runtime-specific action type.
type RuntimeAction = reconciler.Action[*resource.Runtime, *resource.RuntimeState]

// Apply reconciles resources with state and executes actions using DAG-based ordering.
func (e *Engine) Apply(ctx context.Context, resources []resource.Resource) error {
	slog.Debug("applying configuration", "resources", len(resources))

	// Build dependency graph and get execution layers
	resolver := graph.NewResolver()
	for _, res := range resources {
		resolver.AddResource(res)
	}

	layers, err := resolver.Resolve()
	if err != nil {
		return fmt.Errorf("failed to resolve dependencies: %w", err)
	}

	slog.Debug("dependency resolution completed", "layers", len(layers))

	// Acquire lock for execution
	if err := e.store.Lock(); err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer func() { _ = e.store.Unlock() }()

	// Load current state
	st, err := e.store.Load()
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	// Configure resolver after state is loaded (while holding lock)
	if e.resolverConfigurer != nil {
		if err := e.resolverConfigurer(st); err != nil {
			slog.Warn("failed to configure resolver", "error", err)
		}
	}

	// Build resource maps for quick lookup
	resourceMap := buildResourceMap(resources)

	// Register installers for delegation type
	for _, res := range resources {
		if inst, ok := res.(*resource.Installer); ok && inst.InstallerSpec != nil {
			e.toolInstaller.RegisterInstaller(inst.Name(), &tool.InstallerInfo{
				Type:     inst.InstallerSpec.Type,
				ToolRef:  inst.InstallerSpec.ToolRef,
				Commands: inst.InstallerSpec.Commands,
			})
		}
	}

	// Track updated runtimes for taint logic
	updatedRuntimes := make(map[string]bool)
	totalActions := 0

	// Execute layer by layer
	for i, layer := range layers {
		slog.Debug("executing layer", "layer", i, "nodes", len(layer.Nodes))

		if err := e.executeLayer(ctx, layer, resourceMap, st, updatedRuntimes, &totalActions); err != nil {
			return err
		}

		// Reload state after each layer to get updated runtime info
		st, err = e.store.Load()
		if err != nil {
			return fmt.Errorf("failed to reload state: %w", err)
		}

		// Register runtimes for delegation pattern after runtime layer
		for name, runtimeState := range st.Runtimes {
			e.toolInstaller.RegisterRuntime(name, &tool.RuntimeInfo{
				InstallPath: runtimeState.InstallPath,
				ToolBinPath: runtimeState.ToolBinPath,
				Env:         runtimeState.Env,
				Commands:    runtimeState.Commands,
			})
		}
	}

	// Handle taint logic for dependent tools
	if len(updatedRuntimes) > 0 {
		if err := e.handleTaintedTools(ctx, resources, st, updatedRuntimes, &totalActions); err != nil {
			return err
		}
	}

	// Handle removals: resources in state but not in config
	if err := e.handleRemovals(ctx, resources, &totalActions); err != nil {
		return err
	}

	slog.Debug("apply completed", "total_actions", totalActions)
	return nil
}

// executeLayer executes all nodes in a layer.
// Nodes are split by kind: Runtime/Installer execute in parallel first (always before Tools),
// then Tool nodes execute in parallel. Both phases use semaphore-based concurrency limiting.
// If any node fails, all running parallel nodes are canceled immediately.
func (e *Engine) executeLayer(
	ctx context.Context,
	layer graph.Layer,
	resourceMap map[string]resource.Resource,
	st *state.UserState,
	updatedRuntimes map[string]bool,
	totalActions *int,
) error {
	// Split nodes by kind: Runtime/Installer vs Tool
	var runtimeNodes, toolNodes []*graph.Node
	for _, node := range layer.Nodes {
		switch node.Kind {
		case resource.KindRuntime, resource.KindInstaller:
			runtimeNodes = append(runtimeNodes, node)
		default:
			toolNodes = append(toolNodes, node)
		}
	}

	// Phase 1: Execute Runtime/Installer nodes in parallel (always before Tools)
	if err := e.executeNodeGroup(ctx, runtimeNodes, resourceMap, st, updatedRuntimes, totalActions); err != nil {
		return err
	}

	// Phase 2: Execute Tool nodes in parallel
	return e.executeNodeGroup(ctx, toolNodes, resourceMap, st, updatedRuntimes, totalActions)
}

// executeNodeGroup executes a group of nodes, using parallel execution when there are
// multiple nodes and sequential execution for single or empty groups.
func (e *Engine) executeNodeGroup(
	ctx context.Context,
	nodes []*graph.Node,
	resourceMap map[string]resource.Resource,
	st *state.UserState,
	updatedRuntimes map[string]bool,
	totalActions *int,
) error {
	if len(nodes) <= 1 {
		for _, node := range nodes {
			nodeCtx := e.buildNodeContext(ctx, node, resourceMap)
			if err := e.executeNode(nodeCtx, node, resourceMap, st, updatedRuntimes, totalActions); err != nil {
				return err
			}
		}
		return nil
	}
	return e.executeNodesParallel(ctx, nodes, resourceMap, st, updatedRuntimes, totalActions)
}

// executeNodesParallel executes nodes concurrently with cancel-on-error semantics.
// When any node fails, the context is canceled to abort all running nodes.
func (e *Engine) executeNodesParallel(
	ctx context.Context,
	nodes []*graph.Node,
	resourceMap map[string]resource.Resource,
	st *state.UserState,
	updatedRuntimes map[string]bool,
	totalActions *int,
) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sem := semaphore.NewWeighted(int64(e.parallelism))

	var (
		atomicTotal atomic.Int64
		mu          sync.Mutex // protects updatedRuntimes and firstErr
		firstErr    error
		wg          sync.WaitGroup
	)

	for _, node := range nodes {
		// Acquire semaphore before launching goroutine to respect concurrency limit.
		// Check context before blocking on semaphore to exit early on cancellation.
		if err := sem.Acquire(ctx, 1); err != nil {
			// Context was canceled (another goroutine failed)
			break
		}

		wg.Go(func() {
			defer sem.Release(1)

			localUpdated := make(map[string]bool)
			var localActions int

			nodeCtx := e.buildNodeContext(ctx, node, resourceMap)

			if err := e.executeNode(nodeCtx, node, resourceMap, st, localUpdated, &localActions); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				cancel() // Cancel all other running nodes
				return
			}

			atomicTotal.Add(int64(localActions))

			if len(localUpdated) > 0 {
				mu.Lock()
				maps.Copy(updatedRuntimes, localUpdated)
				mu.Unlock()
			}
		})
	}

	wg.Wait()

	*totalActions += int(atomicTotal.Load())

	return firstErr
}

// buildNodeContext creates a context with per-node progress and output callbacks.
// This enables parallel execution where each node has its own isolated callbacks.
func (e *Engine) buildNodeContext(ctx context.Context, node *graph.Node, resourceMap map[string]resource.Resource) context.Context {
	res, ok := resourceMap[graph.NewNodeID(node.Kind, node.Name).String()]
	if !ok {
		return ctx
	}

	switch node.Kind {
	case resource.KindTool:
		t := res.(*resource.Tool)
		method := e.determineInstallMethod(t)
		ctx = download.WithCallback(ctx, download.ProgressCallback(func(downloaded, total int64) {
			e.emitEvent(Event{
				Type:       EventProgress,
				Kind:       resource.KindTool,
				Name:       node.Name,
				Version:    t.ToolSpec.Version,
				Downloaded: downloaded,
				Total:      total,
				Method:     method,
			})
		}))
		ctx = download.WithCallback(ctx, download.OutputCallback(func(line string) {
			e.emitEvent(Event{
				Type:    EventOutput,
				Kind:    resource.KindTool,
				Name:    node.Name,
				Version: t.ToolSpec.Version,
				Output:  line,
				Method:  method,
			})
		}))
	case resource.KindRuntime:
		rt := res.(*resource.Runtime)
		ctx = download.WithCallback(ctx, download.ProgressCallback(func(downloaded, total int64) {
			e.emitEvent(Event{
				Type:       EventProgress,
				Kind:       resource.KindRuntime,
				Name:       node.Name,
				Version:    rt.RuntimeSpec.Version,
				Downloaded: downloaded,
				Total:      total,
			})
		}))
	}
	return ctx
}

// executeNode executes a single node based on its kind.
func (e *Engine) executeNode(
	ctx context.Context,
	node *graph.Node,
	resourceMap map[string]resource.Resource,
	st *state.UserState,
	updatedRuntimes map[string]bool,
	totalActions *int,
) error {
	res, ok := resourceMap[graph.NewNodeID(node.Kind, node.Name).String()]
	if !ok {
		// Node was auto-added as a dependency but not in resources
		slog.Debug("skipping node not in resources", "kind", node.Kind, "name", node.Name)
		return nil
	}

	switch node.Kind {
	case resource.KindRuntime:
		return e.executeRuntimeNode(ctx, res.(*resource.Runtime), st, updatedRuntimes, totalActions)
	case resource.KindInstaller:
		// Installers don't need execution - they're just registered
		return nil
	case resource.KindTool:
		return e.executeToolNode(ctx, res.(*resource.Tool), st, totalActions)
	default:
		slog.Debug("skipping unknown resource kind", "kind", node.Kind, "name", node.Name)
		return nil
	}
}

// executeRuntimeNode executes a runtime action.
func (e *Engine) executeRuntimeNode(
	ctx context.Context,
	rt *resource.Runtime,
	st *state.UserState,
	updatedRuntimes map[string]bool,
	totalActions *int,
) error {
	if e.runtimeExecutor == nil {
		return fmt.Errorf("runtime executor not configured")
	}

	// Build a single-runtime state map to avoid removing other runtimes
	// during per-node reconciliation
	singleRuntimeState := make(map[string]*resource.RuntimeState)
	if rs, exists := st.Runtimes[rt.Name()]; exists {
		singleRuntimeState[rt.Name()] = rs
	}

	// Reconcile single runtime against its own state only
	actions := e.runtimeReconciler.Reconcile([]*resource.Runtime{rt}, singleRuntimeState)
	if len(actions) == 0 {
		return nil
	}

	action := actions[0]
	if action.Type == resource.ActionNone {
		return nil
	}

	// Emit start event
	e.emitEvent(Event{
		Type:    EventStart,
		Kind:    resource.KindRuntime,
		Name:    action.Name,
		Version: rt.RuntimeSpec.Version,
		Action:  action.Type,
	})

	if err := e.runtimeExecutor.Execute(ctx, action); err != nil {
		e.emitEvent(Event{
			Type:   EventError,
			Kind:   resource.KindRuntime,
			Name:   action.Name,
			Action: action.Type,
			Error:  err,
		})
		return fmt.Errorf("failed to execute action %s for runtime %s: %w", action.Type, action.Name, err)
	}

	// Emit complete event
	e.emitEvent(Event{
		Type:   EventComplete,
		Kind:   resource.KindRuntime,
		Name:   action.Name,
		Action: action.Type,
	})

	*totalActions++

	// Track if runtime was updated
	if action.Type == resource.ActionInstall || action.Type == resource.ActionUpgrade || action.Type == resource.ActionReinstall {
		updatedRuntimes[action.Name] = true
	}

	return nil
}

// executeToolNode executes a tool action.
func (e *Engine) executeToolNode(
	ctx context.Context,
	t *resource.Tool,
	st *state.UserState,
	totalActions *int,
) error {
	// Build a single-tool state map to avoid removing other tools
	// during per-node reconciliation
	singleToolState := make(map[string]*resource.ToolState)
	if ts, exists := st.Tools[t.Name()]; exists {
		singleToolState[t.Name()] = ts
	}

	// Reconcile single tool against its own state only
	actions := e.toolReconciler.Reconcile([]*resource.Tool{t}, singleToolState)
	if len(actions) == 0 {
		return nil
	}

	action := actions[0]
	if action.Type == resource.ActionNone {
		return nil
	}

	// Determine install method
	method := e.determineInstallMethod(t)

	// Emit start event
	e.emitEvent(Event{
		Type:    EventStart,
		Kind:    resource.KindTool,
		Name:    action.Name,
		Version: t.ToolSpec.Version,
		Action:  action.Type,
		Method:  method,
	})

	if err := e.toolExecutor.Execute(ctx, action); err != nil {
		e.emitEvent(Event{
			Type:   EventError,
			Kind:   resource.KindTool,
			Name:   action.Name,
			Action: action.Type,
			Error:  err,
			Method: method,
		})
		return fmt.Errorf("failed to execute action %s for tool %s: %w", action.Type, action.Name, err)
	}

	// Emit complete event
	e.emitEvent(Event{
		Type:   EventComplete,
		Kind:   resource.KindTool,
		Name:   action.Name,
		Action: action.Type,
		Method: method,
	})

	*totalActions++
	return nil
}

// determineInstallMethod returns the install method string for a tool.
func (e *Engine) determineInstallMethod(t *resource.Tool) string {
	spec := t.ToolSpec

	// Runtime delegation (e.g., "go install")
	if spec.RuntimeRef != "" {
		return spec.RuntimeRef + " install"
	}

	// Installer delegation (e.g., "brew install")
	if spec.InstallerRef != "" && spec.InstallerRef != "download" {
		return spec.InstallerRef + " install"
	}

	// Download pattern
	return "download"
}

// handleTaintedTools handles reinstallation of tools that depend on updated runtimes.
func (e *Engine) handleTaintedTools(
	ctx context.Context,
	resources []resource.Resource,
	st *state.UserState,
	updatedRuntimes map[string]bool,
	totalActions *int,
) error {
	if err := e.taintDependentTools(st, updatedRuntimes); err != nil {
		slog.Warn("failed to taint dependent tools", "error", err)
		return nil
	}

	// Reload state and re-reconcile tools
	st, err := e.store.Load()
	if err != nil {
		return fmt.Errorf("failed to reload state: %w", err)
	}

	tools := extractTools(resources)
	toolActions := e.toolReconciler.Reconcile(tools, st.Tools)

	for _, action := range toolActions {
		if action.Type == resource.ActionNone {
			continue
		}
		if err := e.toolExecutor.Execute(ctx, action); err != nil {
			return fmt.Errorf("failed to execute action %s for tool %s: %w", action.Type, action.Name, err)
		}
		*totalActions++
	}

	return nil
}

// buildResourceMap creates a map of resources by their node ID.
func buildResourceMap(resources []resource.Resource) map[string]resource.Resource {
	m := make(map[string]resource.Resource)
	for _, res := range resources {
		id := graph.NewNodeID(res.Kind(), res.Name())
		m[id.String()] = res
	}
	return m
}

// handleRemovals processes resources that are in state but not in the config.
func (e *Engine) handleRemovals(ctx context.Context, resources []resource.Resource, totalActions *int) error {
	// Reload state to get latest
	st, err := e.store.Load()
	if err != nil {
		return fmt.Errorf("failed to reload state: %w", err)
	}

	// Get full reconciliation to detect removals
	tools := extractTools(resources)
	runtimes := extractRuntimes(resources)

	toolActions := e.toolReconciler.Reconcile(tools, st.Tools)
	runtimeActions := e.runtimeReconciler.Reconcile(runtimes, st.Runtimes)

	// Execute remove actions for tools
	for _, action := range toolActions {
		if action.Type != resource.ActionRemove {
			continue
		}
		if err := e.toolExecutor.Execute(ctx, action); err != nil {
			return fmt.Errorf("failed to remove tool %s: %w", action.Name, err)
		}
		*totalActions++
	}

	// Execute remove actions for runtimes
	for _, action := range runtimeActions {
		if action.Type != resource.ActionRemove {
			continue
		}
		if err := e.runtimeExecutor.Execute(ctx, action); err != nil {
			return fmt.Errorf("failed to remove runtime %s: %w", action.Name, err)
		}
		*totalActions++
	}

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
		slog.Debug("tainted tools for reinstallation", "count", taintedCount)
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
