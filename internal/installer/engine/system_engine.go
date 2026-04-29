package engine

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/terassyi/tomei/internal/graph"
	"github.com/terassyi/tomei/internal/installer/executor"
	"github.com/terassyi/tomei/internal/installer/reconciler"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/state"
)

// SystemInstallerAction is an alias for system-installer-specific action type.
type SystemInstallerAction = reconciler.Action[*resource.SystemInstaller, *resource.SystemInstallerState]

// SystemPackageRepositoryAction is an alias for system-package-repository-specific action type.
type SystemPackageRepositoryAction = reconciler.Action[*resource.SystemPackageRepository, *resource.SystemPackageRepositoryState]

// SystemPackageSetAction is an alias for system-package-set-specific action type.
type SystemPackageSetAction = reconciler.Action[*resource.SystemPackageSet, *resource.SystemPackageSetState]

// SystemEngine orchestrates the apply process for system-privilege resources.
// It follows the same pattern as Engine but operates on state.SystemState
// and handles SystemInstaller, SystemPackageRepository, and SystemPackageSet resources.
type SystemEngine struct {
	store      *state.Store[state.SystemState]
	stateCache *executor.StateCache[state.SystemState]

	installerStore executor.StateStore[*resource.SystemInstallerState]
	repoStore      executor.StateStore[*resource.SystemPackageRepositoryState]
	packageStore   executor.StateStore[*resource.SystemPackageSetState]

	installerInstaller executor.Installer[*resource.SystemInstaller, *resource.SystemInstallerState]
	repoInstaller      executor.Installer[*resource.SystemPackageRepository, *resource.SystemPackageRepositoryState]
	packageInstaller   executor.Installer[*resource.SystemPackageSet, *resource.SystemPackageSetState]

	installerReconciler *reconciler.Reconciler[*resource.SystemInstaller, *resource.SystemInstallerState]
	repoReconciler      *reconciler.Reconciler[*resource.SystemPackageRepository, *resource.SystemPackageRepositoryState]
	packageReconciler   *reconciler.Reconciler[*resource.SystemPackageSet, *resource.SystemPackageSetState]

	installerExecutor *executor.Executor[*resource.SystemInstaller, *resource.SystemInstallerState]
	repoExecutor      *executor.Executor[*resource.SystemPackageRepository, *resource.SystemPackageRepositoryState]
	packageExecutor   *executor.Executor[*resource.SystemPackageSet, *resource.SystemPackageSetState]

	privilegeHandler PrivilegeHandler
	eventHandler     EventHandler
}

// NewSystemEngine creates a new SystemEngine.
func NewSystemEngine(
	installerInstaller executor.Installer[*resource.SystemInstaller, *resource.SystemInstallerState],
	repoInstaller executor.Installer[*resource.SystemPackageRepository, *resource.SystemPackageRepositoryState],
	packageInstaller executor.Installer[*resource.SystemPackageSet, *resource.SystemPackageSetState],
	store *state.Store[state.SystemState],
) *SystemEngine {
	sc := executor.NewStateCache[state.SystemState](store)
	installerStore := executor.NewSystemInstallerStore(sc)
	repoStore := executor.NewSystemPackageRepositoryStore(sc)
	packageStore := executor.NewSystemPackageSetStore(sc)
	return &SystemEngine{
		store:               store,
		stateCache:          sc,
		installerStore:      installerStore,
		repoStore:           repoStore,
		packageStore:        packageStore,
		installerInstaller:  installerInstaller,
		repoInstaller:       repoInstaller,
		packageInstaller:    packageInstaller,
		installerReconciler: reconciler.NewSystemInstallerReconciler(),
		repoReconciler:      reconciler.NewSystemPackageRepositoryReconciler(),
		packageReconciler:   reconciler.NewSystemPackageSetReconciler(),
		installerExecutor:   executor.New(resource.KindSystemInstaller, installerInstaller, installerStore),
		repoExecutor:        executor.New(resource.KindSystemPackageRepository, repoInstaller, repoStore),
		packageExecutor:     executor.New(resource.KindSystemPackageSet, packageInstaller, packageStore),
	}
}

// SetPrivilegeHandler sets the handler used to indicate that privileged
// operations are allowed. SystemEngine.Apply does not call Acquire or Release;
// the privilege lifecycle is managed by the caller (cmd layer). Presence
// of a handler signals the engine that privileged operations may proceed.
func (e *SystemEngine) SetPrivilegeHandler(handler PrivilegeHandler) {
	e.privilegeHandler = handler
}

// SetEventHandler sets a callback for engine events.
func (e *SystemEngine) SetEventHandler(handler EventHandler) {
	e.eventHandler = handler
}

// emitEvent emits an event to the handler if set.
func (e *SystemEngine) emitEvent(event Event) {
	if e.eventHandler != nil {
		e.eventHandler(event)
	}
}

// Apply reconciles system resources with state and executes actions using DAG-based ordering.
func (e *SystemEngine) Apply(ctx context.Context, resources []resource.Resource) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Extract system resources
	installers := extractByKind[*resource.SystemInstaller](resources)
	repos := extractByKind[*resource.SystemPackageRepository](resources)
	packages := extractByKind[*resource.SystemPackageSet](resources)

	slog.Debug("applying system configuration",
		"installers", len(installers), "repos", len(repos), "packages", len(packages))

	// Build dependency graph
	resolver := graph.NewResolver()
	for _, res := range resources {
		if resource.IsSystemKind(res.Kind()) {
			resolver.AddResource(res)
		}
	}

	layers, err := resolver.Resolve()
	if err != nil {
		return fmt.Errorf("failed to resolve system resource dependencies: %w", err)
	}

	slog.Debug("system dependency resolution completed", "layers", len(layers))

	// System resources require privilege escalation
	if e.privilegeHandler == nil {
		return fmt.Errorf("privilege handler is required for system resource operations")
	}

	// Acquire lock for execution
	if err := e.store.Lock(); err != nil {
		return fmt.Errorf("failed to acquire system state lock: %w", err)
	}
	defer func() { _ = e.store.Unlock() }()

	// Load current state
	st, err := e.store.Load()
	if err != nil {
		return fmt.Errorf("failed to load system state: %w", err)
	}

	// Backup state before changes (non-fatal if fails)
	if err := state.CreateBackup(e.store); err != nil {
		slog.Warn("failed to create system state backup", "error", err)
	}

	// Initialize the in-memory state cache for batch writes
	e.stateCache.Init(st)

	// Build resource map for quick lookup
	resourceMap := buildResourceMap(resources)

	totalActions := 0

	// Build node names for all layers (for event reporting)
	allLayerNodes := make([][]string, len(layers))
	for i, layer := range layers {
		for _, node := range layer.Nodes {
			allLayerNodes[i] = append(allLayerNodes[i], node.ID.String())
		}
	}

	// flushAndReturn is a helper that flushes the state cache (best-effort)
	// before returning an error, so that successfully applied changes are
	// persisted even when a later operation fails or the context is canceled.
	flushAndReturn := func(err error) error {
		if flushErr := e.stateCache.Flush(); flushErr != nil {
			slog.Warn("failed to flush state before returning error", "error", flushErr)
		}
		return err
	}

	// Execute layer by layer (sequential within each layer)
	for i, layer := range layers {
		if ctx.Err() != nil {
			return flushAndReturn(ctx.Err())
		}

		slog.Debug("executing system layer", "layer", i, "nodes", len(layer.Nodes))

		e.emitEvent(Event{
			Type:          EventLayerStart,
			Layer:         i,
			TotalLayers:   len(layers),
			LayerNodes:    allLayerNodes[i],
			AllLayerNodes: allLayerNodes,
		})

		for _, node := range layer.Nodes {
			if ctx.Err() != nil {
				return flushAndReturn(ctx.Err())
			}

			if err := e.executeSystemNode(ctx, node, resourceMap, &totalActions); err != nil {
				// Flush what we have before returning
				if flushErr := e.stateCache.Flush(); flushErr != nil {
					slog.Warn("failed to flush state after error", "error", flushErr)
				}
				return err
			}
		}

		// Flush cached state changes to disk after each layer
		if err := e.stateCache.Flush(); err != nil {
			return fmt.Errorf("failed to flush system state after layer %d: %w", i, err)
		}
	}

	// Handle removals: resources in state but not in config
	if ctx.Err() != nil {
		return flushAndReturn(ctx.Err())
	}
	if err := e.handleSystemRemovals(ctx, installers, repos, packages, &totalActions); err != nil {
		return err
	}

	// Final flush to persist any changes from removals
	if err := e.stateCache.Flush(); err != nil {
		return fmt.Errorf("failed to flush final system state: %w", err)
	}

	slog.Debug("system apply completed", "total_actions", totalActions)
	return nil
}

// executeSystemNode dispatches a single DAG node to the appropriate handler.
func (e *SystemEngine) executeSystemNode(
	ctx context.Context,
	node *graph.Node,
	resourceMap map[string]resource.Resource,
	totalActions *int,
) error {
	res, ok := resourceMap[graph.NewNodeID(node.Kind, node.Name).String()]
	if !ok {
		slog.Debug("skipping system node not in resources", "kind", node.Kind, "name", node.Name)
		return nil
	}

	switch node.Kind {
	case resource.KindSystemInstaller:
		return reconcileAndExecute(ctx, e, node.Kind,
			res.(*resource.SystemInstaller), e.installerStore, e.installerReconciler, e.installerExecutor, totalActions)
	case resource.KindSystemPackageRepository:
		return reconcileAndExecute(ctx, e, node.Kind,
			res.(*resource.SystemPackageRepository), e.repoStore, e.repoReconciler, e.repoExecutor, totalActions)
	case resource.KindSystemPackageSet:
		return reconcileAndExecute(ctx, e, node.Kind,
			res.(*resource.SystemPackageSet), e.packageStore, e.packageReconciler, e.packageExecutor, totalActions)
	default:
		slog.Debug("skipping non-system resource kind", "kind", node.Kind, "name", node.Name)
		return nil
	}
}

// reconcileAndExecute reconciles a single resource against its state and executes the resulting action.
// It is a generic function shared by all three system resource types.
func reconcileAndExecute[R resource.Resource, S resource.State](
	ctx context.Context,
	emitter eventEmitter,
	kind resource.Kind,
	res R,
	store executor.StateStore[S],
	rec *reconciler.Reconciler[R, S],
	exec *executor.Executor[R, S],
	totalActions *int,
) error {
	singleState := make(map[string]S)
	st, exists, err := store.Load(res.Name())
	if err != nil {
		return fmt.Errorf("failed to load %s state for %s: %w", kind, res.Name(), err)
	}
	if exists {
		singleState[res.Name()] = st
	}

	actions := rec.Reconcile([]R{res}, singleState)
	if len(actions) == 0 {
		return nil
	}

	action := actions[0]
	if action.Type == resource.ActionNone {
		return nil
	}

	emitter.emitEvent(Event{
		Type:   EventStart,
		Kind:   kind,
		Name:   action.Name,
		Action: action.Type,
	})

	if err := exec.Execute(ctx, action); err != nil {
		emitter.emitEvent(Event{
			Type:   EventError,
			Kind:   kind,
			Name:   action.Name,
			Action: action.Type,
			Error:  err,
		})
		return fmt.Errorf("failed to execute action %s for %s %s: %w", action.Type, kind, action.Name, err)
	}

	emitter.emitEvent(Event{
		Type:   EventComplete,
		Kind:   kind,
		Name:   action.Name,
		Action: action.Type,
	})

	*totalActions++
	return nil
}

// handleSystemRemovals handles removal of system resources that are in state but not in config.
// Removals happen in reverse dependency order: packages → repos → installers.
func (e *SystemEngine) handleSystemRemovals(
	ctx context.Context,
	installers []*resource.SystemInstaller,
	repos []*resource.SystemPackageRepository,
	packages []*resource.SystemPackageSet,
	totalActions *int,
) error {
	st := e.stateCache.Snapshot()

	packageActions := e.packageReconciler.Reconcile(packages, st.SystemPackages)
	repoActions := e.repoReconciler.Reconcile(repos, st.SystemPackageRepositories)
	installerActions := e.installerReconciler.Reconcile(installers, st.SystemInstallers)

	// Validate removal dependencies
	if err := validateSystemRemovalDeps(installerActions, repoActions, repos, packages); err != nil {
		return err
	}

	// Collect all removal node names for the layer header
	var layerNodes []string
	layerNodes = collectRemovalNodes(layerNodes, resource.KindSystemPackageSet, packageActions)
	layerNodes = collectRemovalNodes(layerNodes, resource.KindSystemPackageRepository, repoActions)
	layerNodes = collectRemovalNodes(layerNodes, resource.KindSystemInstaller, installerActions)

	if len(layerNodes) == 0 {
		return nil
	}

	e.emitEvent(Event{
		Type:       EventLayerStart,
		Phase:      PhaseRemove,
		LayerNodes: layerNodes,
	})

	// Execute removals in reverse dependency order.
	// Flush after each batch so that successfully removed resources are persisted
	// even if a later batch fails — preventing state drift.
	if err := executeRemovals(ctx, e, resource.KindSystemPackageSet, packageActions, e.packageExecutor, totalActions); err != nil {
		if flushErr := e.stateCache.Flush(); flushErr != nil {
			slog.Warn("failed to flush state after package removal error", "error", flushErr)
		}
		return err
	}
	if err := e.stateCache.Flush(); err != nil {
		return fmt.Errorf("failed to flush state after package removals: %w", err)
	}

	if err := executeRemovals(ctx, e, resource.KindSystemPackageRepository, repoActions, e.repoExecutor, totalActions); err != nil {
		if flushErr := e.stateCache.Flush(); flushErr != nil {
			slog.Warn("failed to flush state after repo removal error", "error", flushErr)
		}
		return err
	}
	if err := e.stateCache.Flush(); err != nil {
		return fmt.Errorf("failed to flush state after repo removals: %w", err)
	}

	if err := executeRemovals(ctx, e, resource.KindSystemInstaller, installerActions, e.installerExecutor, totalActions); err != nil {
		if flushErr := e.stateCache.Flush(); flushErr != nil {
			slog.Warn("failed to flush state after installer removal error", "error", flushErr)
		}
		return err
	}
	if err := e.stateCache.Flush(); err != nil {
		return fmt.Errorf("failed to flush state after installer removals: %w", err)
	}
	return nil
}

// validateSystemRemovalDeps checks that no remaining resources reference resources being removed.
func validateSystemRemovalDeps(
	installerActions []SystemInstallerAction,
	repoActions []SystemPackageRepositoryAction,
	remainingRepos []*resource.SystemPackageRepository,
	remainingPackages []*resource.SystemPackageSet,
) error {
	// Check installer removals
	var installerRemovals []string
	for _, action := range installerActions {
		if action.Type == resource.ActionRemove {
			installerRemovals = append(installerRemovals, action.Name)
		}
	}
	if len(installerRemovals) > 0 {
		if err := checkSystemInstallerRemovalDeps(installerRemovals, remainingRepos, remainingPackages); err != nil {
			return err
		}
	}

	// Check repo removals
	var repoRemovals []string
	for _, action := range repoActions {
		if action.Type == resource.ActionRemove {
			repoRemovals = append(repoRemovals, action.Name)
		}
	}
	if len(repoRemovals) > 0 {
		if err := checkSystemRepoRemovalDeps(repoRemovals, remainingPackages); err != nil {
			return err
		}
	}

	return nil
}

// checkSystemInstallerRemovalDeps validates that no remaining repositories or package sets
// reference a system installer being removed.
func checkSystemInstallerRemovalDeps(
	installerRemovals []string,
	remainingRepos []*resource.SystemPackageRepository,
	remainingPackages []*resource.SystemPackageSet,
) error {
	removals := make(map[string]bool, len(installerRemovals))
	for _, name := range installerRemovals {
		removals[name] = true
	}

	for _, repo := range remainingRepos {
		if removals[repo.SystemPackageRepositorySpec.InstallerRef] {
			return fmt.Errorf(
				"cannot remove SystemInstaller %q: SystemPackageRepository %q still references it",
				repo.SystemPackageRepositorySpec.InstallerRef, repo.Name(),
			)
		}
	}
	for _, pkg := range remainingPackages {
		if removals[pkg.SystemPackageSetSpec.InstallerRef] {
			return fmt.Errorf(
				"cannot remove SystemInstaller %q: SystemPackageSet %q still references it",
				pkg.SystemPackageSetSpec.InstallerRef, pkg.Name(),
			)
		}
	}
	return nil
}

// checkSystemRepoRemovalDeps validates that no remaining package sets
// reference a system package repository being removed.
func checkSystemRepoRemovalDeps(
	repoRemovals []string,
	remainingPackages []*resource.SystemPackageSet,
) error {
	removals := make(map[string]bool, len(repoRemovals))
	for _, name := range repoRemovals {
		removals[name] = true
	}

	for _, pkg := range remainingPackages {
		if pkg.SystemPackageSetSpec.RepositoryRef != "" && removals[pkg.SystemPackageSetSpec.RepositoryRef] {
			return fmt.Errorf(
				"cannot remove SystemPackageRepository %q: SystemPackageSet %q still references it",
				pkg.SystemPackageSetSpec.RepositoryRef, pkg.Name(),
			)
		}
	}
	return nil
}

// PlanAll returns system installer, repository, and package set actions
// based on resources and current state.
func (e *SystemEngine) PlanAll(ctx context.Context, resources []resource.Resource) (
	[]SystemInstallerAction,
	[]SystemPackageRepositoryAction,
	[]SystemPackageSetAction,
	error,
) {
	slog.Debug("planning system configuration", "resources", len(resources))

	installers := extractByKind[*resource.SystemInstaller](resources)
	repos := extractByKind[*resource.SystemPackageRepository](resources)
	packages := extractByKind[*resource.SystemPackageSet](resources)

	// Acquire lock for state read
	if err := e.store.Lock(); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to acquire system state lock: %w", err)
	}
	defer func() { _ = e.store.Unlock() }()

	st, err := e.store.Load()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load system state: %w", err)
	}

	// Reconcile each resource type
	installerActions := e.installerReconciler.Reconcile(installers, st.SystemInstallers)
	repoActions := e.repoReconciler.Reconcile(repos, st.SystemPackageRepositories)
	packageActions := e.packageReconciler.Reconcile(packages, st.SystemPackages)

	// Validate removal dependencies
	if err := validateSystemRemovalDeps(installerActions, repoActions, repos, packages); err != nil {
		return nil, nil, nil, err
	}

	slog.Debug("system plan completed",
		"installerActions", len(installerActions),
		"repoActions", len(repoActions),
		"packageActions", len(packageActions),
	)
	return installerActions, repoActions, packageActions, nil
}
