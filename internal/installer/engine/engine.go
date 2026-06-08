package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/terassyi/tomei/internal/age"
	"github.com/terassyi/tomei/internal/graph"
	"github.com/terassyi/tomei/internal/installer/download"
	"github.com/terassyi/tomei/internal/installer/executor"
	"github.com/terassyi/tomei/internal/installer/reconciler"
	"github.com/terassyi/tomei/internal/installer/tool"
	"github.com/terassyi/tomei/internal/path"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/state"
	"golang.org/x/sync/semaphore"
)

// ToolInstaller defines the interface for installing tools.
type ToolInstaller interface {
	Install(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error)
	Remove(ctx context.Context, st *resource.ToolState, name string) error
	RegisterRuntime(name string, info *tool.RuntimeInfo)
	RegisterInstaller(name string, info *tool.InstallerInfo)
	SetToolBinPaths(paths map[string]string)
	SetProgressCallback(callback download.ProgressCallback)
	SetOutputCallback(callback download.OutputCallback)
}

// RuntimeInstaller defines the interface for installing runtimes.
type RuntimeInstaller interface {
	Install(ctx context.Context, res *resource.Runtime, name string) (*resource.RuntimeState, error)
	Remove(ctx context.Context, st *resource.RuntimeState, name string) error
	SetProgressCallback(callback download.ProgressCallback)
}

// InstallerRepositoryInstaller defines the interface for installing installer repositories.
type InstallerRepositoryInstaller interface {
	Install(ctx context.Context, res *resource.InstallerRepository, name string) (*resource.InstallerRepositoryState, error)
	Remove(ctx context.Context, st *resource.InstallerRepositoryState, name string) error
	SetToolBinPaths(paths map[string]string)
}

// ResolverConfigurer is a callback to configure the tool resolver after state is loaded.
// This allows resolver setup to happen after the lock is acquired and state is read.
type ResolverConfigurer func(st *state.UserState) error

// PrivilegeHandler manages the sudo session lifecycle for privileged tool execution.
// The caller (cmd layer) is responsible for calling Acquire/Release and setting
// the handler on the engine via SetPrivilegeHandler before Apply.
type PrivilegeHandler interface {
	// Acquire validates sudo credentials and starts a keepalive goroutine.
	// It should prompt the user for a password if needed (interactive).
	Acquire(ctx context.Context) error

	// Release invalidates the sudo timestamp and stops the keepalive goroutine.
	// Safe to call multiple times (idempotent via sync.Once).
	Release() error
}

// Phase represents the execution phase of the engine.
type Phase int

const (
	// PhaseDAG is the normal dependency-layer execution phase.
	PhaseDAG Phase = iota
	// PhaseTaint is the taint reinstall phase after runtime upgrades.
	PhaseTaint
	// PhaseRemove is the removal phase for dropped resources.
	PhaseRemove
)

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
	// EventLayerStart is emitted at the beginning of each execution layer.
	EventLayerStart
)

// MethodDownload is the value reported in Event.Method for resources that
// install via direct download (no delegation, no runtime).
const MethodDownload = "download"

// Event represents an engine event for progress reporting.
type Event struct {
	Type       EventType
	Phase      Phase // execution phase (default PhaseDAG)
	Kind       resource.Kind
	Name       string
	Version    string
	Action     resource.ActionType
	Error      error
	Downloaded int64  // bytes downloaded (for EventProgress)
	Total      int64  // total bytes (-1 if unknown, for EventProgress)
	Output     string // output line (for EventOutput)
	Method     string // install method: MethodDownload, "go install", etc.

	// EventLayerStart fields
	Layer         int        // current layer index (0-based)
	TotalLayers   int        // total number of layers
	LayerNodes    []string   // node names in the current layer
	AllLayerNodes [][]string // node names for all layers (for rendering pending layer headers)

	// EventComplete fields
	InstallPath string // install path (for EventComplete)
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
	store                   *state.Store[state.UserState]
	stateCache              *executor.StateCache[state.UserState]
	toolStore               executor.StateStore[*resource.ToolState]
	runtimeStore            executor.StateStore[*resource.RuntimeState]
	installerRepoStore      executor.StateStore[*resource.InstallerRepositoryState]
	toolInstaller           ToolInstaller
	runtimeInstaller        RuntimeInstaller
	installerRepoInstaller  InstallerRepositoryInstaller
	runtimeReconciler       *reconciler.Reconciler[*resource.Runtime, *resource.RuntimeState]
	runtimeExecutor         *executor.Executor[*resource.Runtime, *resource.RuntimeState]
	toolReconciler          *reconciler.Reconciler[*resource.Tool, *resource.ToolState]
	toolExecutor            *executor.Executor[*resource.Tool, *resource.ToolState]
	installerRepoReconciler *reconciler.Reconciler[*resource.InstallerRepository, *resource.InstallerRepositoryState]
	installerRepoExecutor   *executor.Executor[*resource.InstallerRepository, *resource.InstallerRepositoryState]
	resolverConfigurer      ResolverConfigurer
	privilegeHandler        PrivilegeHandler
	eventHandler            EventHandler
	parallelism             int
	updateCfg               UpdateConfig
	skippedPrivileged       int // count of privileged removals skipped (no --system)
	// skipUserKindRemovals: when true, handleRemovals skips ActionRemove for
	// non-privileged Tools, Runtimes, and InstallerRepositories. Set by the
	// cmd layer under --system-only so the engine does not tear down
	// user-level state that was installed by a prior plain `tomei apply`.
	// Counterpart of the privilegeHandler==nil skip for priv tools.
	skipUserKindRemovals   bool
	skippedUserKindRemoves int // count of user-kind removals skipped (--system-only)

	// ageFetcher resolves upstream release publication time for the
	// minimumReleaseAge gate. nil disables the gate entirely.
	ageFetcher age.Fetcher
	// skipMu guards skippedReleaseAge and unverifiedReleaseAge, which are
	// appended from parallel tool-node goroutines during Apply.
	skipMu               sync.Mutex
	skippedReleaseAge    []SkipInfo
	unverifiedReleaseAge []UnverifiedInfo
}

// SkipInfo records a tool whose install was skipped because its upstream
// release is younger than the configured minimumReleaseAge.
type SkipInfo struct {
	Kind      resource.Kind
	Name      string
	MinAge    time.Duration
	ActualAge time.Duration
	Source    age.Source
}

// UnverifiedInfo records a tool whose minimumReleaseAge gate was enabled but
// could not be evaluated (fetch error or no upstream timestamp available), and
// which was therefore installed anyway (fail-open). Surfaced so a configured
// supply-chain control failing open is visible rather than silent.
type UnverifiedInfo struct {
	Name   string
	Source age.Source
	Reason string
}

// UpdateConfig holds update-related flags for apply and plan commands.
type UpdateConfig struct {
	// SyncMode taints tools with VersionKind=latest (for --sync).
	SyncMode bool
	// UpdateTools taints tools with VersionKind=latest or alias (for --update-tools).
	UpdateTools bool
	// UpdateRuntimes taints runtimes with VersionKind=alias or latest (for --update-runtimes).
	UpdateRuntimes bool
	// IgnoreMinReleaseAge bypasses the minimumReleaseAge gate (for --ignore-min-release-age).
	IgnoreMinReleaseAge bool
}

// NewEngine creates a new Engine.
func NewEngine(
	toolInstaller ToolInstaller,
	runtimeInstaller RuntimeInstaller,
	installerRepoInstaller InstallerRepositoryInstaller,
	store *state.Store[state.UserState],
) *Engine {
	sc := executor.NewStateCache[state.UserState](store)
	toolStore := executor.NewToolStore(sc)
	runtimeStore := executor.NewRuntimeStore(sc)
	repoStore := executor.NewInstallerRepositoryStore(sc)
	return &Engine{
		store:                   store,
		stateCache:              sc,
		toolStore:               toolStore,
		runtimeStore:            runtimeStore,
		installerRepoStore:      repoStore,
		toolInstaller:           toolInstaller,
		runtimeInstaller:        runtimeInstaller,
		installerRepoInstaller:  installerRepoInstaller,
		runtimeReconciler:       reconciler.NewRuntimeReconciler(),
		runtimeExecutor:         executor.New(resource.KindRuntime, runtimeInstaller, runtimeStore),
		toolReconciler:          reconciler.NewToolReconciler(),
		toolExecutor:            executor.New(resource.KindTool, toolInstaller, toolStore),
		installerRepoReconciler: reconciler.NewInstallerRepositoryReconciler(),
		installerRepoExecutor:   executor.New(resource.KindInstallerRepository, installerRepoInstaller, repoStore),
		parallelism:             DefaultParallelism,
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

// SetUpdateConfig sets the update configuration (sync, update-tools, update-runtimes flags).
func (e *Engine) SetUpdateConfig(cfg UpdateConfig) {
	e.updateCfg = cfg
}

// SetPrivilegeHandler sets the handler used to indicate that privileged
// operations are allowed. Engine.Apply does not call Acquire or Release;
// the privilege lifecycle is managed by the caller (cmd layer). Presence
// of a handler signals the engine that privileged install/removal actions
// may proceed.
func (e *Engine) SetPrivilegeHandler(handler PrivilegeHandler) {
	e.privilegeHandler = handler
}

// SkippedPrivileged returns the number of privileged tool removals that were
// skipped because --system was not used. Call after Apply() completes.
func (e *Engine) SkippedPrivileged() int {
	return e.skippedPrivileged
}

// SetAgeFetcher sets the release-age fetcher backing the minimumReleaseAge
// gate. A nil fetcher (the default) disables the gate entirely.
func (e *Engine) SetAgeFetcher(f age.Fetcher) {
	e.ageFetcher = f
}

// SkippedReleaseAge returns tools whose install was skipped because their
// upstream release was younger than the configured minimumReleaseAge. Call
// after Apply() completes.
func (e *Engine) SkippedReleaseAge() []SkipInfo {
	e.skipMu.Lock()
	defer e.skipMu.Unlock()
	return slices.Clone(e.skippedReleaseAge)
}

// UnverifiedReleaseAge returns tools whose minimumReleaseAge gate was enabled
// but could not be evaluated (and which were installed anyway). Call after
// Apply() completes.
func (e *Engine) UnverifiedReleaseAge() []UnverifiedInfo {
	e.skipMu.Lock()
	defer e.skipMu.Unlock()
	return slices.Clone(e.unverifiedReleaseAge)
}

// SetSkipUserKindRemovals enables the symmetric counterpart of the priv-
// removal skip for --system-only mode. When true, handleRemovals filters
// out ActionRemove for non-privileged Tools, Runtimes, and
// InstallerRepositories so that state installed by a prior `tomei apply`
// is preserved across `tomei apply --system-only`. Privileged Tool
// removals are unaffected (they're in scope under --system-only).
//
// As a safety side-effect, tool removals whose stored state is nil
// (corrupted/partial state) are also skipped — privilege cannot be
// determined and the installer's Remove would likely deref nil. These
// nil-state skips are logged at Warn and do NOT increment
// SkippedUserKindRemoves (the counter is reserved for confirmed
// user-kind skips).
func (e *Engine) SetSkipUserKindRemovals(skip bool) {
	e.skipUserKindRemovals = skip
}

// SkippedUserKindRemoves returns the number of user-kind removals
// (non-priv Tool, Runtime, InstallerRepository) skipped by
// SetSkipUserKindRemovals. Call after Apply() completes.
func (e *Engine) SkippedUserKindRemoves() int {
	return e.skippedUserKindRemoves
}

// eventEmitter is an unexported interface for emitting engine events.
// Both Engine and SystemEngine satisfy this interface.
type eventEmitter interface {
	emitEvent(event Event)
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

// InstallerRepositoryAction is an alias for installer-repository-specific action type.
type InstallerRepositoryAction = reconciler.Action[*resource.InstallerRepository, *resource.InstallerRepositoryState]

// Apply reconciles resources with state and executes actions using DAG-based ordering.
func (e *Engine) Apply(ctx context.Context, resources []resource.Resource) error {
	// Short-circuit if context is already canceled (e.g., signal received)
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Reset per-run counters so the values reported by accessors like
	// SkippedPrivileged() reflect only this Apply invocation, not prior runs
	// when the Engine instance is reused (e.g., in tests or orchestration).
	e.skippedPrivileged = 0
	e.skippedUserKindRemoves = 0
	// Guards a concurrent SkippedReleaseAge()/UnverifiedReleaseAge() reader;
	// Apply itself is not re-entrant (store.Lock + wg.Wait serialize it).
	e.skipMu.Lock()
	e.skippedReleaseAge = nil
	e.unverifiedReleaseAge = nil
	e.skipMu.Unlock()

	// Expand set resources (ToolSet, etc.) into individual resources
	var err error
	resources, err = resource.ExpandSets(resources)
	if err != nil {
		return fmt.Errorf("failed to expand sets: %w", err)
	}

	slog.Debug("applying configuration", "resources", len(resources))

	// Reject user-declared Installer/aqua or Installer/download with a
	// mismatched spec.type before AppendBuiltinInstallers silently picks
	// up the override and changes the install mechanism.
	if err := resource.ValidateBuiltinInstallerOverrides(resources); err != nil {
		return fmt.Errorf("apply rejected: %w", err)
	}

	// Build dependency graph and get execution layers.
	// Inject builtin installers into the resolver only so that dependency
	// nodes like "Installer/aqua" are properly resolved. Builtins are NOT
	// added to the resources slice to avoid persisting them to state.
	resolver := graph.NewResolver()
	for _, res := range AppendBuiltinInstallers(resources) {
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

	// Backup state before changes (non-fatal if fails)
	if err := state.CreateBackup(e.store); err != nil {
		slog.Warn("failed to create state backup", "error", err)
	}

	// Configure resolver after state is loaded (while holding lock)
	if e.resolverConfigurer != nil {
		if err := e.resolverConfigurer(st); err != nil {
			slog.Warn("failed to configure resolver", "error", err)
		}
	}

	// Apply taint marks based on update flags
	applyUpdateTaints(st, e.updateCfg)

	// Build resource maps for quick lookup
	resourceMap := buildResourceMap(resources)

	// Collect the spec-declared minimumReleaseAge per runtime, keyed by name, so the
	// runtime-delegation tool-install path can expose it as {{.MinimumReleaseAge}}.
	// Sourced from the live manifest (not persisted state) and validated here, since
	// RuntimeSpec.Validate() is otherwise not called on the apply path (unlike
	// InstallerSpec, validated below) — this keeps an unparseable/negative duration
	// from reaching a delegation shell command.
	runtimeMinReleaseAge := make(map[string]string)
	for _, rt := range extractByKind[*resource.Runtime](resources) {
		if rt.RuntimeSpec == nil {
			continue
		}
		// Parse to validate; the value is threaded as a raw string below.
		if _, err := rt.RuntimeSpec.ParsedMinimumReleaseAge(); err != nil {
			return fmt.Errorf("invalid runtime %q: %w", rt.Name(), err)
		}
		runtimeMinReleaseAge[rt.Name()] = rt.RuntimeSpec.MinimumReleaseAge
	}

	// Register installers for delegation type and save to state
	for _, res := range resources {
		if inst, ok := res.(*resource.Installer); ok && inst.InstallerSpec != nil {
			if err := inst.InstallerSpec.Validate(); err != nil {
				return fmt.Errorf("invalid installer %q: %w", inst.Name(), err)
			}
			e.toolInstaller.RegisterInstaller(inst.Name(), &tool.InstallerInfo{
				Type:              inst.InstallerSpec.Type,
				ToolRef:           inst.InstallerSpec.ToolRef,
				Commands:          inst.InstallerSpec.Commands,
				MinimumReleaseAge: inst.InstallerSpec.MinimumReleaseAge,
			})
			// Persist installer state (including ToolRef and BinDir) for removal lookup and env
			if st.Installers == nil {
				st.Installers = make(map[string]*resource.InstallerState)
			}
			var expandedBinDir string
			if inst.InstallerSpec.BinDir != "" {
				var err error
				expandedBinDir, err = path.Expand(inst.InstallerSpec.BinDir)
				if err != nil {
					return fmt.Errorf("failed to expand binDir for installer %q: %w", inst.Name(), err)
				}
			}
			// Preserve existing state fields (e.g., Version) that are not set here
			existing := st.Installers[inst.Name()]
			newState := &resource.InstallerState{
				ToolRef:   inst.InstallerSpec.ToolRef,
				BinDir:    expandedBinDir,
				UpdatedAt: time.Now(),
			}
			if existing != nil {
				newState.Version = existing.Version
			}
			st.Installers[inst.Name()] = newState
		}
	}
	if err := e.store.Save(st); err != nil {
		return fmt.Errorf("failed to save installer state: %w", err)
	}

	// Initialize the in-memory state cache for batch writes
	e.stateCache.Init(st)

	// Track updated runtimes for taint logic
	updatedRuntimes := make(map[string]bool)
	totalActions := 0

	// Build node names for all layers
	allLayerNodes := make([][]string, len(layers))
	for i, layer := range layers {
		for _, node := range layer.Nodes {
			allLayerNodes[i] = append(allLayerNodes[i], node.ID.String())
		}
	}

	// Execute layer by layer
	for i, layer := range layers {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		slog.Debug("executing layer", "layer", i, "nodes", len(layer.Nodes))

		// Emit layer start event for progress UI
		e.emitEvent(Event{
			Type:          EventLayerStart,
			Layer:         i,
			TotalLayers:   len(layers),
			LayerNodes:    allLayerNodes[i],
			AllLayerNodes: allLayerNodes,
		})

		layerErr := e.executeLayer(ctx, layer, resourceMap, updatedRuntimes, &totalActions)

		// Flush cached state changes to disk after each layer, even on error.
		// This persists successfully installed tools for idempotent retries.
		if err := e.stateCache.Flush(); err != nil {
			return fmt.Errorf("failed to flush state after layer %d: %w", i, err)
		}

		if layerErr != nil {
			return layerErr
		}

		// Use snapshot for inter-layer state reads
		st = e.stateCache.Snapshot()

		// Register runtimes for delegation pattern after runtime layer
		for name, runtimeState := range st.Runtimes {
			e.toolInstaller.RegisterRuntime(name, &tool.RuntimeInfo{
				InstallPath: runtimeState.InstallPath,
				BinDir:      runtimeState.BinDir,
				ToolBinPath: runtimeState.ToolBinPath,
				Env:         runtimeState.Env,
				Commands:    runtimeState.Commands,
				// Spec-sourced (live manifest), not state — see runtimeMinReleaseAge above.
				MinimumReleaseAge: runtimeMinReleaseAge[name],
			})
		}
	}

	// Handle taint logic for dependent tools
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(updatedRuntimes) > 0 {
		if err := e.handleTaintedTools(ctx, resources, updatedRuntimes, &totalActions); err != nil {
			return err
		}
	}

	// Handle removals: resources in state but not in config
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := e.handleRemovals(ctx, resources, &totalActions); err != nil {
		return err
	}

	// Final flush to persist any changes from taint handling and removals
	if err := e.stateCache.Flush(); err != nil {
		return fmt.Errorf("failed to flush final state: %w", err)
	}

	slog.Debug("apply completed", "total_actions", totalActions)
	return nil
}

// executeLayer executes all nodes in a layer.
// Nodes are split by kind into three phases:
//
//	Phase 1: Runtime/Installer nodes (always first)
//	Phase 2: InstallerRepository nodes (after installers are ready)
//	Phase 3: Tool nodes (after repositories are configured)
//
// Each phase uses semaphore-based concurrency limiting.
// If any node fails, all running parallel nodes are canceled immediately.
func (e *Engine) executeLayer(
	ctx context.Context,
	layer graph.Layer,
	resourceMap map[string]resource.Resource,
	updatedRuntimes map[string]bool,
	totalActions *int,
) error {
	// Split nodes by kind into three groups
	var runtimeNodes, repoNodes, toolNodes []*graph.Node
	for _, node := range layer.Nodes {
		switch node.Kind {
		case resource.KindRuntime, resource.KindInstaller:
			runtimeNodes = append(runtimeNodes, node)
		case resource.KindInstallerRepository:
			repoNodes = append(repoNodes, node)
		default:
			toolNodes = append(toolNodes, node)
		}
	}

	// Phase 1: Execute Runtime/Installer nodes in parallel (always before repos and tools)
	if err := e.executeNodeGroup(ctx, runtimeNodes, resourceMap, updatedRuntimes, totalActions); err != nil {
		return err
	}

	// Update tool bin paths for InstallerRepository delegation commands.
	// After Phase 1, toolRef targets are installed and their binPaths are in state.
	st := e.stateCache.Snapshot()
	e.updateToolBinPaths(resourceMap, st)

	// Phase 2: Execute InstallerRepository nodes in parallel (after installers, before tools)
	if err := e.executeNodeGroup(ctx, repoNodes, resourceMap, updatedRuntimes, totalActions); err != nil {
		return err
	}

	// Phase 3: Execute Tool nodes with delegation serialization.
	// Tools installed via runtime delegation share global state within the
	// package manager, so concurrent invocations can corrupt it.
	return e.executeToolNodesWithDelegationSerialization(ctx, toolNodes, resourceMap, updatedRuntimes, totalActions)
}

// executeNodeGroup executes a group of nodes, using parallel execution when there are
// multiple nodes and sequential execution for single or empty groups.
func (e *Engine) executeNodeGroup(
	ctx context.Context,
	nodes []*graph.Node,
	resourceMap map[string]resource.Resource,
	updatedRuntimes map[string]bool,
	totalActions *int,
) error {
	if len(nodes) <= 1 {
		for _, node := range nodes {
			nodeCtx := e.buildNodeContext(ctx, node, resourceMap)
			if err := e.executeNode(nodeCtx, node, resourceMap, updatedRuntimes, totalActions); err != nil {
				return err
			}
		}
		return nil
	}
	return e.executeNodesParallel(ctx, nodes, resourceMap, updatedRuntimes, totalActions)
}

// executeNodesParallel executes nodes concurrently with continue-on-error semantics.
// When a node fails, other nodes in the same layer continue to completion.
// All errors are collected and returned as a joined error.
func (e *Engine) executeNodesParallel(
	ctx context.Context,
	nodes []*graph.Node,
	resourceMap map[string]resource.Resource,
	updatedRuntimes map[string]bool,
	totalActions *int,
) error {
	sem := semaphore.NewWeighted(int64(e.parallelism))

	var (
		atomicTotal atomic.Int64
		mu          sync.Mutex // protects updatedRuntimes and errs
		errs        []error
		wg          sync.WaitGroup
	)

	for _, node := range nodes {
		// Acquire semaphore before launching goroutine to respect concurrency limit.
		// Parent context cancellation (e.g., SIGINT) still causes early exit here.
		if err := sem.Acquire(ctx, 1); err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
			break
		}

		wg.Go(func() {
			defer sem.Release(1)

			localUpdated := make(map[string]bool)
			var localActions int

			nodeCtx := e.buildNodeContext(ctx, node, resourceMap)

			if err := e.executeNode(nodeCtx, node, resourceMap, localUpdated, &localActions); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
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

	return errors.Join(errs...)
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
		ctx = download.WithCallback(ctx, download.OutputCallback(func(line string) {
			e.emitEvent(Event{
				Type:    EventOutput,
				Kind:    resource.KindRuntime,
				Name:    node.Name,
				Version: rt.RuntimeSpec.Version,
				Output:  line,
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
		return e.executeRuntimeNode(ctx, res.(*resource.Runtime), updatedRuntimes, totalActions)
	case resource.KindInstaller:
		// Installers don't need execution - they're just registered
		return nil
	case resource.KindInstallerRepository:
		return e.executeInstallerRepositoryNode(ctx, res.(*resource.InstallerRepository), totalActions)
	case resource.KindTool:
		return e.executeToolNode(ctx, res.(*resource.Tool), resourceMap, totalActions)
	default:
		slog.Debug("skipping unknown resource kind", "kind", node.Kind, "name", node.Name)
		return nil
	}
}

// executeRuntimeNode executes a runtime action.
func (e *Engine) executeRuntimeNode(
	ctx context.Context,
	rt *resource.Runtime,
	updatedRuntimes map[string]bool,
	totalActions *int,
) error {
	if e.runtimeExecutor == nil {
		return fmt.Errorf("runtime executor not configured")
	}

	// Build a single-runtime state map to avoid removing other runtimes
	// during per-node reconciliation.
	// Use runtimeStore.Load() for mutex-safe access during parallel execution.
	singleRuntimeState := make(map[string]*resource.RuntimeState)
	rs, exists, err := e.runtimeStore.Load(rt.Name())
	if err != nil {
		return fmt.Errorf("failed to load runtime state for %s: %w", rt.Name(), err)
	}
	if exists {
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

	// Load updated state to get install path
	var runtimeInstallPath string
	if updatedRS, exists, loadErr := e.runtimeStore.Load(rt.Name()); loadErr == nil && exists {
		runtimeInstallPath = updatedRS.InstallPath
	}

	// Emit complete event
	e.emitEvent(Event{
		Type:        EventComplete,
		Kind:        resource.KindRuntime,
		Name:        action.Name,
		Action:      action.Type,
		InstallPath: runtimeInstallPath,
	})

	*totalActions++

	// Track if runtime was upgraded (not first install) and the version actually changed.
	// Only version changes should trigger taint on dependent tools.
	// This prevents unnecessary tool cascade when --update-runtimes re-resolves
	// an alias to the same version.
	if action.Type == resource.ActionUpgrade {
		oldVersion := ""
		if action.State != nil {
			oldVersion = action.State.Version
		}
		newRS, newExists, loadErr := e.runtimeStore.Load(rt.Name())
		if loadErr == nil && newExists && newRS.Version != oldVersion {
			updatedRuntimes[action.Name] = true
		}
	}

	return nil
}

// updateToolBinPaths builds and sets the mapping from installer name to tool bin directory.
// This ensures delegation commands can find toolRef binaries in PATH.
// It first checks resources (for install/apply), then falls back to state (for removals
// where the Installer resource may no longer be in the manifest).
func (e *Engine) updateToolBinPaths(resourceMap map[string]resource.Resource, st *state.UserState) {
	toolBinPaths := make(map[string]string)
	// From resources (available during install/apply)
	for _, res := range resourceMap {
		inst, ok := res.(*resource.Installer)
		if !ok || inst.InstallerSpec == nil || inst.InstallerSpec.ToolRef == "" {
			continue
		}
		if ts, exists := st.Tools[inst.InstallerSpec.ToolRef]; exists && ts.BinPath != "" {
			toolBinPaths[inst.Name()] = filepath.Dir(ts.BinPath)
		}
	}
	// From state (fallback for removals when Installer is no longer in manifest)
	for name, instState := range st.Installers {
		if _, already := toolBinPaths[name]; already {
			continue
		}
		if instState.ToolRef == "" {
			continue
		}
		if ts, exists := st.Tools[instState.ToolRef]; exists && ts.BinPath != "" {
			toolBinPaths[name] = filepath.Dir(ts.BinPath)
		}
	}
	e.installerRepoInstaller.SetToolBinPaths(toolBinPaths)
	e.toolInstaller.SetToolBinPaths(toolBinPaths)
}

// executeInstallerRepositoryNode executes an installer repository action.
func (e *Engine) executeInstallerRepositoryNode(
	ctx context.Context,
	repo *resource.InstallerRepository,
	totalActions *int,
) error {
	if e.installerRepoExecutor == nil {
		return fmt.Errorf("installer repository executor not configured")
	}

	// Build a single-repo state map to avoid removing other repos.
	// Use installerRepoStore.Load() for mutex-safe access during parallel execution.
	singleRepoState := make(map[string]*resource.InstallerRepositoryState)
	rs, exists, err := e.installerRepoStore.Load(repo.Name())
	if err != nil {
		return fmt.Errorf("failed to load installer repository state for %s: %w", repo.Name(), err)
	}
	if exists {
		singleRepoState[repo.Name()] = rs
	}

	// Reconcile single repo against its own state only
	actions := e.installerRepoReconciler.Reconcile([]*resource.InstallerRepository{repo}, singleRepoState)
	if len(actions) == 0 {
		return nil
	}

	action := actions[0]
	if action.Type == resource.ActionNone {
		return nil
	}

	// Emit start event
	e.emitEvent(Event{
		Type:   EventStart,
		Kind:   resource.KindInstallerRepository,
		Name:   action.Name,
		Action: action.Type,
	})

	if err := e.installerRepoExecutor.Execute(ctx, action); err != nil {
		e.emitEvent(Event{
			Type:   EventError,
			Kind:   resource.KindInstallerRepository,
			Name:   action.Name,
			Action: action.Type,
			Error:  err,
		})
		return fmt.Errorf("failed to execute action %s for installer repository %s: %w", action.Type, action.Name, err)
	}

	// Emit complete event
	e.emitEvent(Event{
		Type:   EventComplete,
		Kind:   resource.KindInstallerRepository,
		Name:   action.Name,
		Action: action.Type,
	})

	*totalActions++
	return nil
}

// executeToolNode executes a tool action.
func (e *Engine) executeToolNode(
	ctx context.Context,
	t *resource.Tool,
	resourceMap map[string]resource.Resource,
	totalActions *int,
) error {
	// Build a single-tool state map to avoid removing other tools
	// during per-node reconciliation.
	// Use toolStore.Load() for mutex-safe access during parallel execution.
	singleToolState := make(map[string]*resource.ToolState)
	ts, exists, err := e.toolStore.Load(t.Name())
	if err != nil {
		return fmt.Errorf("failed to load tool state for %s: %w", t.Name(), err)
	}
	if exists {
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

	// minimumReleaseAge gate: skip installs/upgrades/reinstalls whose upstream
	// release is younger than the configured threshold. Skipped tools are not
	// executed, not written to state, and not counted (no error).
	switch action.Type {
	case resource.ActionInstall, resource.ActionUpgrade, resource.ActionReinstall:
		if e.checkReleaseAgeGate(ctx, t, resourceMap) {
			return nil
		}
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

	// Load updated state to get install path
	var toolInstallPath string
	if updatedTS, exists, loadErr := e.toolStore.Load(t.Name()); loadErr == nil && exists {
		toolInstallPath = updatedTS.BinPath
	}

	// Emit complete event
	e.emitEvent(Event{
		Type:        EventComplete,
		Kind:        resource.KindTool,
		Name:        action.Name,
		Action:      action.Type,
		Method:      method,
		InstallPath: toolInstallPath,
	})

	*totalActions++
	return nil
}

// lookupInstaller resolves the Installer resource a tool references by name.
// Returns nil for an un-overridden builtin (aqua/download), which is absent
// from resources (and thus from resourceMap).
func lookupInstaller(ref string, resourceMap map[string]resource.Resource) *resource.Installer {
	if ref == "" {
		return nil
	}
	if res, ok := resourceMap[graph.NewNodeID(resource.KindInstaller, ref).String()]; ok {
		if inst, ok := res.(*resource.Installer); ok {
			return inst
		}
	}
	return nil
}

// releaseAgeKeyAndThreshold classifies a tool for the minimumReleaseAge gate.
// It is pure (no I/O). enabled is false when the gate does not apply: commands
// pattern, runtime delegation, delegation installer, no fetchable source, or a
// zero/unset threshold.
//
// Classification is by installer TYPE, not name: any download-type installer
// (builtin aqua/download, an override, or a user-declared type:download
// installer) is gateable. The source is chosen by what the tool carries — a
// registry package (owner/repo) uses the GitHub Releases published_at; an
// explicit Source.URL uses the HTTP Last-Modified header.
//
// v1 limitation: the aqua tag is taken directly from spec.Version. aqua
// registries often apply version_prefix/trimV, so the GitHub tag may differ;
// on mismatch GetReleaseByTag 404s and the gate fails open (surfaced via
// UnverifiedReleaseAge). Registry-based tag resolution is a follow-up.
func (e *Engine) releaseAgeKeyAndThreshold(
	t *resource.Tool,
	resourceMap map[string]resource.Resource,
) (age.Key, time.Duration, bool) {
	spec := t.ToolSpec

	// Commands pattern and runtime delegation are the tool's own / the user
	// command's responsibility (#253), never gated here.
	if spec.Commands != nil || spec.RuntimeRef != "" {
		return age.Key{}, 0, false
	}

	inst := lookupInstaller(spec.InstallerRef, resourceMap)
	if inst != nil && inst.InstallerSpec != nil && inst.InstallerSpec.Type.IsDelegation() {
		return age.Key{}, 0, false
	}

	var key age.Key
	switch {
	case spec.Package.IsRegistry():
		key = age.Key{
			Source: age.SourceAquaGitHubReleases,
			Owner:  spec.Package.Owner,
			Repo:   spec.Package.Repo,
			Tag:    spec.Version,
		}
	case spec.Source != nil && spec.Source.URL != "":
		key = age.Key{Source: age.SourceLastModified, URL: spec.Source.URL}
	default:
		return age.Key{}, 0, false
	}

	// Threshold comes from the referenced installer's spec. An un-overridden
	// builtin (inst == nil) carries no threshold, so the gate is opt-in.
	if inst == nil || inst.InstallerSpec == nil {
		return age.Key{}, 0, false
	}
	minAge, err := inst.InstallerSpec.ParsedMinimumReleaseAge()
	if err != nil || minAge == 0 {
		return age.Key{}, 0, false
	}
	return key, minAge, true
}

// checkReleaseAgeGate reports whether a tool install should be SKIPPED because
// its upstream release is younger than the configured minimumReleaseAge.
//
// Fail-open: if the gate is enabled but the release time cannot be fetched
// (network error, 404 tag mismatch, missing header), the install proceeds and
// the tool is recorded via UnverifiedReleaseAge so the failure is visible
// rather than silent.
func (e *Engine) checkReleaseAgeGate(
	ctx context.Context,
	t *resource.Tool,
	resourceMap map[string]resource.Resource,
) bool {
	if e.ageFetcher == nil || e.updateCfg.IgnoreMinReleaseAge {
		return false
	}
	key, minAge, enabled := e.releaseAgeKeyAndThreshold(t, resourceMap)
	if !enabled {
		return false
	}

	publishedAt, ok, err := e.ageFetcher.Fetch(ctx, key)
	if err != nil {
		slog.Warn("minimumReleaseAge gate could not be evaluated; installing anyway",
			"tool", t.Name(), "source", key.Source, "error", err)
		e.recordUnverified(UnverifiedInfo{Name: t.Name(), Source: key.Source, Reason: "fetch error: " + err.Error()})
		return false
	}
	if !ok {
		slog.Warn("minimumReleaseAge gate could not be evaluated; installing anyway",
			"tool", t.Name(), "source", key.Source, "reason", "no release timestamp available")
		e.recordUnverified(UnverifiedInfo{Name: t.Name(), Source: key.Source, Reason: "no release timestamp available"})
		return false
	}

	actualAge := time.Since(publishedAt)
	if actualAge >= minAge {
		return false
	}

	// Warn (not Info) so a fired gate is never wholly silent under --quiet,
	// which suppresses the fmt summary but not Warn-level logs.
	slog.Warn("skipping install: release younger than minimumReleaseAge",
		"tool", t.Name(), "source", key.Source, "minAge", minAge, "actualAge", actualAge)
	e.recordSkip(SkipInfo{
		Kind: resource.KindTool, Name: t.Name(),
		MinAge: minAge, ActualAge: actualAge, Source: key.Source,
	})
	return true
}

func (e *Engine) recordSkip(s SkipInfo) {
	e.skipMu.Lock()
	defer e.skipMu.Unlock()
	e.skippedReleaseAge = append(e.skippedReleaseAge, s)
}

func (e *Engine) recordUnverified(u UnverifiedInfo) {
	e.skipMu.Lock()
	defer e.skipMu.Unlock()
	e.unverifiedReleaseAge = append(e.unverifiedReleaseAge, u)
}

// determineInstallMethod returns the install method string for a tool.
func (e *Engine) determineInstallMethod(t *resource.Tool) string {
	spec := t.ToolSpec

	// Self-managed tool (commands pattern)
	if spec.Commands != nil {
		return "commands"
	}

	// Runtime delegation (e.g., "go install")
	if spec.RuntimeRef != "" {
		return spec.RuntimeRef + " install"
	}

	// Installer delegation (e.g., "brew install")
	if spec.InstallerRef != "" && spec.InstallerRef != MethodDownload {
		return spec.InstallerRef + " install"
	}

	// Download pattern
	return MethodDownload
}

// delegationKeyForTool returns the serialization group key for a tool.
// Tools with the same non-empty key must be installed sequentially to avoid
// concurrent package manager invocations corrupting shared state (e.g.,
// concurrent `go install` or `pnpm add -g`).
// Returns "" for download-pattern tools and commands-pattern tools that can
// run fully in parallel. Commands-pattern tools have no shared package manager
// state, so they need no serialization.
func delegationKeyForTool(t *resource.Tool, resourceMap map[string]resource.Resource) string {
	// RuntimeRef takes precedence (e.g., "go install", "cargo install")
	if t.ToolSpec.RuntimeRef != "" {
		return "runtime:" + t.ToolSpec.RuntimeRef
	}
	// Check InstallerRef for delegation-type installers via resource map
	if ref := t.ToolSpec.InstallerRef; ref != "" {
		if inst := lookupInstaller(ref, resourceMap); inst != nil &&
			inst.InstallerSpec != nil && inst.InstallerSpec.Type.IsDelegation() {
			return "installer:" + ref
		}
	}
	return "" // download pattern
}

// partitionToolsByDelegation splits tool nodes into download nodes (fully parallel)
// and delegation groups (sequential within each group, parallel across groups).
func partitionToolsByDelegation(
	nodes []*graph.Node,
	resourceMap map[string]resource.Resource,
) (downloadNodes []*graph.Node, delegationGroups [][]*graph.Node) {
	groups := make(map[string][]*graph.Node)
	for _, node := range nodes {
		res, ok := resourceMap[graph.NewNodeID(node.Kind, node.Name).String()]
		if !ok || node.Kind != resource.KindTool {
			downloadNodes = append(downloadNodes, node)
			continue
		}
		t := res.(*resource.Tool)
		key := delegationKeyForTool(t, resourceMap)
		if key == "" {
			downloadNodes = append(downloadNodes, node)
		} else {
			groups[key] = append(groups[key], node)
		}
	}
	// Sort groups by key for deterministic ordering (aids debugging and log reproducibility)
	for _, k := range slices.Sorted(maps.Keys(groups)) {
		delegationGroups = append(delegationGroups, groups[k])
	}
	return downloadNodes, delegationGroups
}

// executeToolNodesWithDelegationSerialization executes tool nodes with delegation
// groups serialized. Download-pattern tools run fully in parallel. Tools sharing
// the same delegation key (e.g., same RuntimeRef) run sequentially within the group
// but different groups run in parallel, all under the global semaphore.
func (e *Engine) executeToolNodesWithDelegationSerialization(
	ctx context.Context,
	nodes []*graph.Node,
	resourceMap map[string]resource.Resource,
	updatedRuntimes map[string]bool,
	totalActions *int,
) error {
	downloadNodes, delegationGroups := partitionToolsByDelegation(nodes, resourceMap)

	// Fast path: no delegation groups — fully parallel as before
	if len(delegationGroups) == 0 {
		return e.executeNodeGroup(ctx, downloadNodes, resourceMap, updatedRuntimes, totalActions)
	}

	// Fast path: no download nodes and a single delegation group — sequential.
	// This uses fail-fast (returns on first error) because a failed package manager
	// invocation may leave shared state (GOPATH, module cache) in a broken state,
	// making subsequent installs in the same group unreliable.
	if len(downloadNodes) == 0 && len(delegationGroups) == 1 {
		for _, node := range delegationGroups[0] {
			nodeCtx := e.buildNodeContext(ctx, node, resourceMap)
			if err := e.executeNode(nodeCtx, node, resourceMap, updatedRuntimes, totalActions); err != nil {
				return err
			}
		}
		return nil
	}

	// Mixed execution: download tools + delegation groups under shared semaphore
	sem := semaphore.NewWeighted(int64(e.parallelism))

	var (
		atomicTotal atomic.Int64
		mu          sync.Mutex // protects updatedRuntimes and errs
		errs        []error
		wg          sync.WaitGroup
	)

	// Launch download tools as individual goroutines (same as executeNodesParallel)
	for _, node := range downloadNodes {
		if err := sem.Acquire(ctx, 1); err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
			break
		}

		wg.Go(func() {
			defer sem.Release(1)

			localUpdated := make(map[string]bool)
			var localActions int

			nodeCtx := e.buildNodeContext(ctx, node, resourceMap)

			if err := e.executeNode(nodeCtx, node, resourceMap, localUpdated, &localActions); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
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

	// Launch each delegation group as a single goroutine with internal sequential execution.
	// Within a group, a failure stops remaining tools because a failed package manager
	// invocation may leave shared state in a broken state.
	for _, group := range delegationGroups {
		if ctx.Err() != nil {
			break
		}

		wg.Go(func() {
			for i, node := range group {
				// Acquire semaphore per tool to maintain fair scheduling with download tools
				if err := sem.Acquire(ctx, 1); err != nil {
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
					return
				}

				localUpdated := make(map[string]bool)
				var localActions int

				nodeCtx := e.buildNodeContext(ctx, node, resourceMap)
				err := e.executeNode(nodeCtx, node, resourceMap, localUpdated, &localActions)

				sem.Release(1)

				if err != nil {
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()

					// Log only tools that were actually skipped (after the failed one)
					for _, remaining := range group[i+1:] {
						slog.Debug("skipping delegation tool due to group error",
							"tool", remaining.Name, "error", err)
					}
					return
				}

				atomicTotal.Add(int64(localActions))

				if len(localUpdated) > 0 {
					mu.Lock()
					maps.Copy(updatedRuntimes, localUpdated)
					mu.Unlock()
				}
			}
		})
	}

	wg.Wait()

	*totalActions += int(atomicTotal.Load())

	return errors.Join(errs...)
}

// handleTaintedTools handles reinstallation of tools that depend on updated runtimes.
// NOTE: Tainted tools are reinstalled sequentially in a simple loop, which implicitly
// provides delegation serialization safety. If this is ever parallelized, it must
// respect delegation group boundaries (see executeToolNodesWithDelegationSerialization).
func (e *Engine) handleTaintedTools(
	ctx context.Context,
	resources []resource.Resource,
	updatedRuntimes map[string]bool,
	totalActions *int,
) error {
	st := e.stateCache.Snapshot()
	e.taintDependentTools(st, updatedRuntimes)

	// Flush tainted state to disk, then use snapshot for re-reconciliation
	if err := e.stateCache.Flush(); err != nil {
		return fmt.Errorf("failed to flush tainted state: %w", err)
	}
	st = e.stateCache.Snapshot()

	tools := extractByKind[*resource.Tool](resources)
	toolActions := e.toolReconciler.Reconcile(tools, st.Tools)

	// Collect non-None actions and build layer node names for UI
	var activeActions []reconciler.Action[*resource.Tool, *resource.ToolState]
	var layerNodes []string
	for _, action := range toolActions {
		if action.Type == resource.ActionNone {
			continue
		}
		activeActions = append(activeActions, action)
		layerNodes = append(layerNodes, fmt.Sprintf("%s/%s", resource.KindTool, action.Name))
	}

	if len(activeActions) == 0 {
		return nil
	}

	// Emit layer start for taint phase
	e.emitEvent(Event{
		Type:       EventLayerStart,
		Phase:      PhaseTaint,
		LayerNodes: layerNodes,
	})

	for _, action := range activeActions {
		t := action.Resource
		method := e.determineInstallMethod(t)

		e.emitEvent(Event{
			Type:    EventStart,
			Phase:   PhaseTaint,
			Kind:    resource.KindTool,
			Name:    action.Name,
			Version: t.ToolSpec.Version,
			Action:  action.Type,
			Method:  method,
		})

		if err := e.toolExecutor.Execute(ctx, action); err != nil {
			e.emitEvent(Event{
				Type:   EventError,
				Phase:  PhaseTaint,
				Kind:   resource.KindTool,
				Name:   action.Name,
				Action: action.Type,
				Error:  err,
				Method: method,
			})
			return fmt.Errorf("failed to execute action %s for tool %s: %w", action.Type, action.Name, err)
		}

		// Load updated state to get install path
		var toolInstallPath string
		if updatedTS, exists, loadErr := e.toolStore.Load(t.Name()); loadErr == nil && exists {
			toolInstallPath = updatedTS.BinPath
		}

		e.emitEvent(Event{
			Type:        EventComplete,
			Phase:       PhaseTaint,
			Kind:        resource.KindTool,
			Name:        action.Name,
			Action:      action.Type,
			Method:      method,
			InstallPath: toolInstallPath,
		})

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
// Removal order: Tools first, then InstallerRepositories, then Runtimes.
func (e *Engine) handleRemovals(ctx context.Context, resources []resource.Resource, totalActions *int) error {
	// Use snapshot for current state (may include unflushed changes)
	st := e.stateCache.Snapshot()

	// Get full reconciliation to detect removals
	tools := extractByKind[*resource.Tool](resources)
	runtimes := extractByKind[*resource.Runtime](resources)
	repos := extractByKind[*resource.InstallerRepository](resources)

	toolActions := e.toolReconciler.Reconcile(tools, st.Tools)
	repoActions := e.installerRepoReconciler.Reconcile(repos, st.InstallerRepositories)
	runtimeActions := e.runtimeReconciler.Reconcile(runtimes, st.Runtimes)

	// Skip removal of privileged tools when no PrivilegeHandler is set (--system not used).
	// Privileged tools require sudo for removal; without --system, warn and skip.
	if e.privilegeHandler == nil {
		filtered := make([]ToolAction, 0, len(toolActions))
		for _, action := range toolActions {
			if action.Type == resource.ActionRemove && action.State != nil && action.State.Privileged {
				slog.Warn("skipping removal of privileged tool (use --system or --system-only)",
					"name", action.Name,
					"reason", action.State.PrivilegedRemovalReason())
				e.skippedPrivileged++
				continue
			}
			filtered = append(filtered, action)
		}
		toolActions = filtered
	}

	// --system-only: skip removals of non-priv Tools, Runtimes, and
	// InstallerRepositories. These were filtered out of the manifest at
	// the cmd layer (filterNonPrivilegedWithLog in cmd/tomei/apply.go), so
	// the reconciler sees their state entries as orphaned and emits
	// ActionRemove. Skipping them here preserves the prior `tomei apply`
	// installation across `tomei apply --system-only`. Priv Tool removals
	// are NOT skipped — privilege is in scope under --system-only.
	if e.skipUserKindRemovals {
		filteredTools := make([]ToolAction, 0, len(toolActions))
		for _, action := range toolActions {
			if action.Type != resource.ActionRemove {
				filteredTools = append(filteredTools, action)
				continue
			}
			switch {
			case action.State == nil:
				// Partial/corrupted state: we can't determine privilege.
				// Skip removal conservatively (the installer's Remove would
				// likely deref the nil state and panic) and log distinctly
				// so this case is not conflated with non-priv tools. Do NOT
				// increment skippedUserKindRemoves — this is unknown-
				// privilege, not necessarily a user-kind, and would mislead
				// the counter.
				slog.Warn("skipping tool removal with nil state (--system-only)",
					"name", action.Name)
			case !action.State.Privileged:
				slog.Info("skipping removal of non-privileged tool (--system-only)",
					"name", action.Name)
				e.skippedUserKindRemoves++
			default:
				// Privileged removal: in scope under --system-only, keep it.
				filteredTools = append(filteredTools, action)
			}
		}
		toolActions = filteredTools

		filteredRuntimes := make([]RuntimeAction, 0, len(runtimeActions))
		for _, action := range runtimeActions {
			if action.Type == resource.ActionRemove {
				slog.Info("skipping removal of runtime (--system-only)",
					"name", action.Name)
				e.skippedUserKindRemoves++
				continue
			}
			filteredRuntimes = append(filteredRuntimes, action)
		}
		runtimeActions = filteredRuntimes

		filteredRepos := make([]InstallerRepositoryAction, 0, len(repoActions))
		for _, action := range repoActions {
			if action.Type == resource.ActionRemove {
				slog.Info("skipping removal of installer repository (--system-only)",
					"name", action.Name)
				e.skippedUserKindRemoves++
				continue
			}
			filteredRepos = append(filteredRepos, action)
		}
		repoActions = filteredRepos
	}

	// Validate no remaining tools depend on runtimes being removed
	var runtimeRemovals []string
	for _, action := range runtimeActions {
		if action.Type == resource.ActionRemove {
			runtimeRemovals = append(runtimeRemovals, action.Name)
		}
	}
	if len(runtimeRemovals) > 0 {
		if err := checkRemovalDependencies(runtimeRemovals, tools); err != nil {
			return err
		}
	}

	// Collect all removal node names for the layer header
	var layerNodes []string
	layerNodes = collectRemovalNodes(layerNodes, resource.KindTool, toolActions)
	layerNodes = collectRemovalNodes(layerNodes, resource.KindInstallerRepository, repoActions)
	layerNodes = collectRemovalNodes(layerNodes, resource.KindRuntime, runtimeActions)

	if len(layerNodes) == 0 {
		return nil
	}

	// Emit layer start for removal phase
	e.emitEvent(Event{
		Type:       EventLayerStart,
		Phase:      PhaseRemove,
		LayerNodes: layerNodes,
	})

	// Execute remove actions: tools first, then repos, then runtimes
	if err := executeRemovals(ctx, e, resource.KindTool, toolActions, e.toolExecutor, totalActions); err != nil {
		return err
	}

	// Update tool bin paths for InstallerRepository remove commands (e.g., helm repo remove)
	e.updateToolBinPaths(buildResourceMap(resources), st)

	if err := executeRemovals(ctx, e, resource.KindInstallerRepository, repoActions, e.installerRepoExecutor, totalActions); err != nil {
		return err
	}

	return executeRemovals(ctx, e, resource.KindRuntime, runtimeActions, e.runtimeExecutor, totalActions)
}

// collectRemovalNodes appends node names for removal actions to the slice.
func collectRemovalNodes[R resource.Resource, S resource.State](
	nodes []string,
	kind resource.Kind,
	actions []reconciler.Action[R, S],
) []string {
	for _, action := range actions {
		if action.Type == resource.ActionRemove {
			nodes = append(nodes, fmt.Sprintf("%s/%s", kind, action.Name))
		}
	}
	return nodes
}

// executeRemovals iterates over actions, executing removals with PhaseRemove events.
func executeRemovals[R resource.Resource, S resource.State](
	ctx context.Context,
	emitter eventEmitter,
	kind resource.Kind,
	actions []reconciler.Action[R, S],
	exec *executor.Executor[R, S],
	totalActions *int,
) error {
	for _, action := range actions {
		if action.Type != resource.ActionRemove {
			continue
		}
		emitter.emitEvent(Event{
			Type:   EventStart,
			Phase:  PhaseRemove,
			Kind:   kind,
			Name:   action.Name,
			Action: action.Type,
		})
		if err := exec.Execute(ctx, action); err != nil {
			emitter.emitEvent(Event{
				Type:   EventError,
				Phase:  PhaseRemove,
				Kind:   kind,
				Name:   action.Name,
				Action: action.Type,
				Error:  err,
			})
			return fmt.Errorf("failed to remove %s %s: %w", kind, action.Name, err)
		}
		emitter.emitEvent(Event{
			Type:   EventComplete,
			Phase:  PhaseRemove,
			Kind:   kind,
			Name:   action.Name,
			Action: action.Type,
		})
		*totalActions++
	}
	return nil
}

// PlanAll returns runtime, installer repository, and tool actions based on resources and current state.
func (e *Engine) PlanAll(ctx context.Context, resources []resource.Resource) ([]RuntimeAction, []InstallerRepositoryAction, []ToolAction, error) {
	slog.Debug("planning configuration", "resources", len(resources))

	// Extract resources
	runtimes := extractByKind[*resource.Runtime](resources)
	repos := extractByKind[*resource.InstallerRepository](resources)
	tools := extractByKind[*resource.Tool](resources)

	// Acquire lock for state read
	if err := e.store.Lock(); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to acquire lock: %w", err)
	}

	// Load current state
	st, err := e.store.Load()
	if err != nil {
		_ = e.store.Unlock()
		return nil, nil, nil, fmt.Errorf("failed to load state: %w", err)
	}

	_ = e.store.Unlock()

	// Apply taint marks based on update flags (same as Apply)
	applyUpdateTaints(st, e.updateCfg)

	// Reconcile runtimes
	var runtimeActions []RuntimeAction
	if e.runtimeReconciler != nil {
		runtimeActions = e.runtimeReconciler.Reconcile(runtimes, st.Runtimes)
	}

	// Reconcile installer repositories
	var repoActions []InstallerRepositoryAction
	if e.installerRepoReconciler != nil {
		repoActions = e.installerRepoReconciler.Reconcile(repos, st.InstallerRepositories)
	}

	// Reconcile tools
	toolActions := e.toolReconciler.Reconcile(tools, st.Tools)

	// Validate no remaining tools depend on runtimes being removed
	var runtimeRemovals []string
	for _, action := range runtimeActions {
		if action.Type == resource.ActionRemove {
			runtimeRemovals = append(runtimeRemovals, action.Name)
		}
	}
	if len(runtimeRemovals) > 0 {
		if err := checkRemovalDependencies(runtimeRemovals, tools); err != nil {
			return nil, nil, nil, err
		}
	}

	slog.Debug("plan completed", "runtimeActions", len(runtimeActions), "repoActions", len(repoActions), "toolActions", len(toolActions))
	return runtimeActions, repoActions, toolActions, nil
}

// taintDependentTools marks tools that depend on the updated runtimes for reinstallation.
// Tainted state is written to the cache via toolStore.Save() and flushed later.
func (e *Engine) taintDependentTools(st *state.UserState, updatedRuntimes map[string]bool) {
	taintedCount := 0
	for name, toolState := range st.Tools {
		if toolState.RuntimeRef == "" || !updatedRuntimes[toolState.RuntimeRef] {
			continue
		}
		// Only taint if the runtime has TaintOnUpgrade enabled
		rs, ok := st.Runtimes[toolState.RuntimeRef]
		if !ok || !rs.TaintOnUpgrade {
			continue
		}
		toolState.Taint(resource.TaintReasonRuntimeUpgraded)
		_ = e.toolStore.Save(name, toolState)
		taintedCount++
		slog.Debug("tainted tool due to runtime upgrade", "tool", name, "runtime", toolState.RuntimeRef)
	}

	if taintedCount > 0 {
		slog.Debug("tainted tools for reinstallation", "count", taintedCount)
	}
}

// ApplyUpdateTaints applies taint marks to state based on update flags.
// Called before stateCache.Init(), so it modifies st directly.
// Exported for use by plan command.
func ApplyUpdateTaints(st *state.UserState, cfg UpdateConfig) {
	applyUpdateTaints(st, cfg)
}

func applyUpdateTaints(st *state.UserState, cfg UpdateConfig) {
	isNonExact := func(vk resource.VersionKind) bool {
		return vk == resource.VersionLatest || vk == resource.VersionAlias
	}

	if cfg.SyncMode {
		taintMatching(st.Tools, func(s *resource.ToolState) bool {
			return s.VersionKind == resource.VersionLatest
		}, resource.TaintReasonSyncUpdate, "tool")
	}
	if cfg.UpdateTools {
		taintMatching(st.Tools, func(s *resource.ToolState) bool {
			return isNonExact(s.VersionKind)
		}, resource.TaintReasonUpdateRequested, "tool")
	}
	if cfg.UpdateRuntimes {
		taintMatching(st.Runtimes, func(s *resource.RuntimeState) bool {
			return isNonExact(s.VersionKind)
		}, resource.TaintReasonUpdateRequested, "runtime")
	}
}

// taintable is the constraint for state types that support taint marking.
type taintable interface {
	Taint(resource.TaintReason)
}

// taintMatching iterates state entries and taints those matching the predicate.
func taintMatching[S taintable](states map[string]S, match func(S) bool, reason resource.TaintReason, kind string) {
	count := 0
	for name, s := range states {
		if match(s) {
			s.Taint(reason)
			count++
			slog.Debug("tainted "+kind+" for update", kind, name)
		}
	}
	if count > 0 {
		slog.Debug("tainted "+kind+"s for update", "count", count)
	}
}

// extractByKind filters resources of a specific concrete type from a list.
func extractByKind[R resource.Resource](resources []resource.Resource) []R {
	var result []R
	for _, res := range resources {
		if r, ok := res.(R); ok {
			result = append(result, r)
		}
	}
	return result
}

// checkRemovalDependencies validates that no remaining tools depend on runtimes being removed.
func checkRemovalDependencies(runtimeRemovals []string, remainingTools []*resource.Tool) error {
	removingRuntimes := make(map[string]bool, len(runtimeRemovals))
	for _, name := range runtimeRemovals {
		removingRuntimes[name] = true
	}

	var blocked []string
	for _, t := range remainingTools {
		if t.ToolSpec.RuntimeRef != "" && removingRuntimes[t.ToolSpec.RuntimeRef] {
			blocked = append(blocked, fmt.Sprintf("tool %q depends on runtime %q", t.Name(), t.ToolSpec.RuntimeRef))
		}
	}

	if len(blocked) > 0 {
		return fmt.Errorf("cannot remove runtime: dependent tools still in spec:\n  %s", strings.Join(blocked, "\n  "))
	}
	return nil
}

// AppendBuiltinInstallers adds builtin installer resources (download, aqua)
// to the resource list if they are not already present. This ensures that
// DAG dependency nodes like "Installer/aqua" have a real resource backing them.
func AppendBuiltinInstallers(resources []resource.Resource) []resource.Resource {
	existing := make(map[string]bool)
	for _, res := range resources {
		if res.Kind() == resource.KindInstaller {
			existing[res.Name()] = true
		}
	}

	for _, inst := range []*resource.Installer{
		download.BuiltinInstaller,
		download.BuiltinAquaInstaller,
	} {
		if !existing[inst.Name()] {
			resources = append(resources, inst)
		}
	}

	return resources
}
