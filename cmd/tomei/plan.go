package main

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/terassyi/tomei/internal/config"
	"github.com/terassyi/tomei/internal/github"
	"github.com/terassyi/tomei/internal/graph"
	"github.com/terassyi/tomei/internal/installer/engine"
	"github.com/terassyi/tomei/internal/installer/reconciler"
	"github.com/terassyi/tomei/internal/path"
	"github.com/terassyi/tomei/internal/registry/aqua"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/state"
)

var planCmd = &cobra.Command{
	Use:   "plan <files or directories...>",
	Short: "Show the execution plan",
	Long: `Show the execution plan without applying changes.

Compares CUE manifests (desired state) with the current state and shows
what actions would be taken by "tomei apply":
  - install: New resources to install
  - upgrade: Resources with version changes
  - reinstall: Tainted resources to reinstall (e.g., runtime upgrade, update flags)
  - remove: Resources in state but not in manifests
  - skip: Resources disabled via enabled: false

Resources are shown in dependency order as a tree. Execution layers
show which resources run in parallel.

Use --output json or --output yaml for machine-readable output
(suitable for scripting and programmatic consumption).`,
	Args: cobra.MinimumNArgs(1),
	RunE: runPlan,
}

// planConfig holds configuration for the plan command.
type planConfig struct {
	loadConfig
	outputFormat string
}

var planCfg planConfig

func init() {
	planCfg.registerFlags(planCmd)
	planCmd.Flags().StringVarP(&planCfg.outputFormat, "output", "o", outputText, "Output format: text, json, yaml")
	_ = planCmd.RegisterFlagCompletionFunc("output", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{outputText, outputJSON, "yaml"}, cobra.ShellCompDirectiveNoFileComp
	})
}

func runPlan(cmd *cobra.Command, args []string) error {
	// Disable color if --no-color flag is set
	if planCfg.noColor {
		color.NoColor = true
	}

	// Sync registry if --sync or --update-tools/--update-all flag is set
	if planCfg.syncRegistry || planCfg.updateTools || planCfg.updateAll {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		if err := syncRegistryForPlan(ctx); err != nil {
			slog.Warn("failed to sync aqua registry", "error", err)
		}
	}

	// Load configuration
	loader := config.NewLoader(nil, planCfg.verifierOpts()...)
	resources, err := loader.LoadPaths(args)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	if len(resources) == 0 {
		cmd.Println("No resources found")
		return nil
	}

	// Collect disabled resources before expansion (for plan display)
	disabledResources := resource.CollectDisabled(resources)

	// Expand set resources (ToolSet, etc.) into individual resources
	resources, err = resource.ExpandSets(resources)
	if err != nil {
		return fmt.Errorf("failed to expand sets: %w", err)
	}

	scope := scopeFromFlags()

	// Reject overlapping SystemPackageSet declarations before plan emits
	// an Upgrade/Remove that would tear down a multi-owner package.
	// Gated on IncludesSystemKinds() because outside --system / --system-only
	// the system resources are forced to ActionSkip by buildResourceInfo
	// and overlap is moot. See resource.ValidateSystemPackageSetOverlap.
	if scope.IncludesSystemKinds() {
		if err := resource.ValidateSystemPackageSetOverlap(resources); err != nil {
			return fmt.Errorf("plan rejected: %w", err)
		}
	}

	updateCfg := engine.UpdateConfig{
		SyncMode:       planCfg.syncRegistry,
		UpdateTools:    planCfg.updateTools || planCfg.updateAll,
		UpdateRuntimes: planCfg.updateRuntimes || planCfg.updateAll,
	}
	result, err := resolvePlan(resources, updateCfg, scope)
	if err != nil {
		return err
	}

	// Inject disabled resource info into the plan
	addDisabledResourceInfo(result.resourceInfo, disabledResources)

	// Output based on format
	switch planCfg.outputFormat {
	case outputJSON:
		exporter := graph.NewExporter(result.filteredLayers, result.resourceInfo, result.edges)
		return exporter.ExportJSON(os.Stdout)
	case "yaml":
		exporter := graph.NewExporter(result.filteredLayers, result.resourceInfo, result.edges)
		return exporter.ExportYAML(os.Stdout)
	case outputText:
		fallthrough
	default:
		return printTextPlan(cmd, args, resources, result)
	}
}

func buildResourceInfo(resources []resource.Resource, updCfg engine.UpdateConfig, scope ApplyScope) map[graph.NodeID]graph.ResourceInfo {
	info := make(map[graph.NodeID]graph.ResourceInfo)

	// Load config and state
	var userState *state.UserState
	var pathConfig *path.Paths
	cfg, err := config.LoadConfig(config.DefaultConfigDir)
	if err == nil {
		pc, err := path.NewFromConfig(cfg)
		if err == nil {
			pathConfig = pc
			store, err := state.NewStore[state.UserState](pathConfig.UserDataDir())
			if err == nil {
				loaded, err := store.LoadReadOnly()
				if err == nil {
					userState = loaded
				}
			}
		}
	}

	// Fall back to default paths when config is unavailable, so system
	// resource plans can still compute SystemDataDir accurately.
	if pathConfig == nil {
		if pc, err := path.New(); err == nil {
			pathConfig = pc
		}
	}

	if userState == nil {
		fmt.Fprintln(os.Stderr, "Warning: tomei is not initialized. Run 'tomei init' for accurate state comparison.")
	}

	// Apply taint logic based on update flags (for plan preview)
	if userState != nil {
		engine.ApplyUpdateTaints(userState, updCfg)
	}

	for _, res := range resources {
		nodeID := graph.NewNodeID(res.Kind(), res.Name())

		// System resources are added separately via addSystemResourceInfo using
		// the read-only store and reconcilers, so skip them in this per-resource loop.
		if resource.IsSystemKind(res.Kind()) {
			continue
		}

		resInfo := graph.ResourceInfo{
			Kind:       res.Kind(),
			Name:       res.Name(),
			Action:     resource.ActionInstall, // default to install
			Privileged: resource.IsPrivileged(res),
		}

		// Get version from spec
		switch res.Kind() {
		case resource.KindRuntime:
			if rt, ok := res.(*resource.Runtime); ok && rt.RuntimeSpec != nil {
				resInfo.Version = rt.RuntimeSpec.Version
			}
		case resource.KindTool:
			if tool, ok := res.(*resource.Tool); ok && tool.ToolSpec != nil {
				resInfo.Version = tool.ToolSpec.Version
			}
		}

		// In --system-only mode, non-privileged user-level resources are
		// out of scope. Mark them as skip and bypass state comparison so
		// the plan reflects what apply will actually do.
		if !scope.IncludesUserKinds() && !resInfo.Privileged {
			resInfo.Action = resource.ActionSkip
			info[nodeID] = resInfo
			continue
		}

		// Determine action by comparing with state
		if userState != nil {
			switch res.Kind() {
			case resource.KindRuntime:
				if rt, ok := userState.Runtimes[res.Name()]; ok {
					if rt.IsTainted() {
						resInfo.Action = resource.ActionReinstall
					} else if rt.Version == resInfo.Version {
						resInfo.Action = resource.ActionNone
					} else {
						resInfo.Action = resource.ActionUpgrade
					}
				}
			case resource.KindTool:
				if tool, ok := userState.Tools[res.Name()]; ok {
					if tool.IsTainted() {
						resInfo.Action = resource.ActionReinstall
					} else if tool.Version == resInfo.Version {
						resInfo.Action = resource.ActionNone
					} else {
						resInfo.Action = resource.ActionUpgrade
					}
				}
			case resource.KindInstaller:
				// Installers don't have versions in state typically
				resInfo.Action = resource.ActionNone
			}
		}

		// Mark privileged tools as skip when privileged is not in scope
		// (i.e., default user mode). Consistent with the system-kind skip
		// pattern below.
		markPrivilegedAsSkip(&resInfo, res, scope.IncludesPrivileged())

		info[nodeID] = resInfo
	}

	// Handle system resources
	if scope.IncludesSystemKinds() && pathConfig != nil {
		addSystemResourceInfo(info, resources, pathConfig)
	} else {
		// System kinds out of scope — mark them all as skip.
		for _, res := range resources {
			if resource.IsSystemKind(res.Kind()) {
				nodeID := graph.NewNodeID(res.Kind(), res.Name())
				info[nodeID] = graph.ResourceInfo{
					Kind:   res.Kind(),
					Name:   res.Name(),
					Action: resource.ActionSkip,
				}
			}
		}
	}

	// Detect removals: resources in state but not in manifests
	if userState != nil {
		for name, rt := range userState.Runtimes {
			nodeID := graph.NewNodeID(resource.KindRuntime, name)
			if _, exists := info[nodeID]; !exists {
				// LoadReadOnly skips validation; defend against a nil
				// entry from a corrupted state.json so plan stays robust.
				if rt == nil {
					slog.Warn("skipping nil runtime entry in state", "name", name)
					info[nodeID] = graph.ResourceInfo{
						Kind:   resource.KindRuntime,
						Name:   name,
						Action: resource.ActionSkip,
					}
					continue
				}
				action := resource.ActionRemove
				// Runtimes are user-kind; --system-only skips their removal.
				if !scope.IncludesUserKinds() {
					action = resource.ActionSkip
				}
				info[nodeID] = graph.ResourceInfo{
					Kind:    resource.KindRuntime,
					Name:    name,
					Version: rt.Version,
					Action:  action,
				}
			}
		}
		for name, tool := range userState.Tools {
			nodeID := graph.NewNodeID(resource.KindTool, name)
			if _, exists := info[nodeID]; !exists {
				if tool == nil {
					slog.Warn("skipping nil tool entry in state", "name", name)
					info[nodeID] = graph.ResourceInfo{
						Kind:   resource.KindTool,
						Name:   name,
						Action: resource.ActionSkip,
					}
					continue
				}
				action := resource.ActionRemove
				switch {
				case tool.Privileged && !scope.IncludesPrivileged():
					// Privileged tool removals require --system or
					// --system-only; outside those, skip.
					action = resource.ActionSkip
				case !tool.Privileged && !scope.IncludesUserKinds():
					// Non-priv tool removals require user kinds in scope;
					// --system-only skips them.
					action = resource.ActionSkip
				}
				info[nodeID] = graph.ResourceInfo{
					Kind:       resource.KindTool,
					Name:       name,
					Version:    tool.Version,
					Action:     action,
					Privileged: tool.Privileged,
				}
			}
		}

		// Predict taint reinstalls: if a runtime with TaintOnUpgrade is being upgraded,
		// tools that depend on it (via RuntimeRef) will be reinstalled.
		// Build a map of runtime specs to check TaintOnUpgrade from the manifest.
		runtimeSpecs := map[string]*resource.RuntimeSpec{}
		upgradedRuntimes := map[string]bool{}
		for _, res := range resources {
			if res.Kind() == resource.KindRuntime {
				if rt, ok := res.(*resource.Runtime); ok && rt.RuntimeSpec != nil {
					runtimeSpecs[res.Name()] = rt.RuntimeSpec
				}
				nodeID := graph.NewNodeID(res.Kind(), res.Name())
				if ri, ok := info[nodeID]; ok && (ri.Action == resource.ActionUpgrade || ri.Action == resource.ActionReinstall) {
					upgradedRuntimes[res.Name()] = true
				}
			}
		}
		if len(upgradedRuntimes) > 0 {
			for name, toolState := range userState.Tools {
				if toolState.RuntimeRef == "" || !upgradedRuntimes[toolState.RuntimeRef] {
					continue
				}
				// Check TaintOnUpgrade from the runtime spec in the manifest
				spec, ok := runtimeSpecs[toolState.RuntimeRef]
				if !ok || !spec.TaintOnUpgrade {
					continue
				}
				nodeID := graph.NewNodeID(resource.KindTool, name)
				if ri, ok := info[nodeID]; ok && ri.Action == resource.ActionNone {
					ri.Action = resource.ActionReinstall
					info[nodeID] = ri
				}
			}
		}
	}

	return info
}

func printTextPlan(cmd *cobra.Command, args []string, resources []resource.Resource, result *planResult) error {
	cmd.Printf("Planning changes for %v\n\n", args)
	cmd.Printf("Found %d resource(s)\n\n", len(resources))

	// Print dependency tree, split into Tools / Privileged Tools / System sections
	w := cmd.OutOrStdout()
	printer := graph.NewTreePrinter(w, planCfg.noColor)
	renderSectionedTree(printer, w, result)

	// Print execution layers
	printer.PrintLayers(result.filteredLayers, result.resourceInfo)

	// Print disabled resources
	disabledInfos := collectSkipInfos(result.resourceInfo)
	if len(disabledInfos) > 0 {
		printer.PrintDisabled(disabledInfos)
	}

	// Print summary
	printer.PrintSummary(result.resourceInfo)

	return nil
}

// planResult holds the resolved plan state.
type planResult struct {
	resolver       graph.Resolver
	filteredLayers []graph.Layer
	resourceInfo   map[graph.NodeID]graph.ResourceInfo
	edges          []graph.Edge
}

// resolvePlan builds the dependency graph, resolves execution layers, and
// computes resource actions from the current state.
func resolvePlan(resources []resource.Resource, updateCfg engine.UpdateConfig, scope ApplyScope) (*planResult, error) {
	definedResources := make(map[string]struct{})
	for _, res := range resources {
		id := graph.NewNodeID(res.Kind(), res.Name())
		definedResources[id.String()] = struct{}{}
	}

	// Inject builtin installers into the resolver only so that dependency
	// nodes like "Installer/aqua" are properly resolved.
	resolver := graph.NewResolver()
	for _, res := range engine.AppendBuiltinInstallers(resources) {
		resolver.AddResource(res)
	}

	layers, err := resolver.Resolve()
	if err != nil {
		return nil, err
	}

	var filteredLayers []graph.Layer
	for _, layer := range layers {
		var filteredNodes []*graph.Node
		for _, node := range layer.Nodes {
			id := graph.NewNodeID(node.Kind, node.Name).String()
			if _, ok := definedResources[id]; ok {
				filteredNodes = append(filteredNodes, node)
			}
		}
		if len(filteredNodes) > 0 {
			filteredLayers = append(filteredLayers, graph.Layer{Nodes: filteredNodes})
		}
	}

	resourceInfo := buildResourceInfo(resources, updateCfg, scope)

	return &planResult{
		resolver:       resolver,
		filteredLayers: filteredLayers,
		resourceInfo:   resourceInfo,
		edges:          resolver.GetEdges(),
	}, nil
}

// planForResources runs the plan logic on already-loaded resources and
// writes the text plan to w. It returns true if there are any changes
// (install, upgrade, reinstall, or remove).
func planForResources(w io.Writer, resources []resource.Resource, disableColor bool, updateCfg engine.UpdateConfig, scope ApplyScope) (bool, error) {
	result, err := resolvePlan(resources, updateCfg, scope)
	if err != nil {
		return false, err
	}

	// Count only real change actions; ActionNone and ActionSkip don't run
	// anything, so they shouldn't trigger a confirmation prompt in apply.
	hasChanges := false
	for _, info := range result.resourceInfo {
		switch info.Action {
		case resource.ActionInstall, resource.ActionUpgrade, resource.ActionReinstall, resource.ActionRemove:
			hasChanges = true
		}
		if hasChanges {
			break
		}
	}

	fmt.Fprintf(w, "Found %d resource(s)\n\n", len(resources))
	printer := graph.NewTreePrinter(w, disableColor)
	renderSectionedTree(printer, w, result)
	printer.PrintLayers(result.filteredLayers, result.resourceInfo)
	printer.PrintSummary(result.resourceInfo)

	return hasChanges, nil
}

// renderSectionedTree prints the dependency tree split into three subsections
// keyed by privilege/system-kind: regular Tools, Privileged Tools (require
// --system), and System resources. Empty sections are skipped. A node present
// in the resolver but absent from result.resourceInfo (e.g. builtin
// Installers added by engine.AppendBuiltinInstallers) gets a zero-value
// ResourceInfo and therefore routes to "Tools:".
func renderSectionedTree(printer *graph.TreePrinter, w io.Writer, result *planResult) {
	type section struct {
		heading string
		include func(graph.NodeID, graph.ResourceInfo) bool
	}
	sections := []section{
		{"Tools:", func(_ graph.NodeID, info graph.ResourceInfo) bool {
			return !info.Privileged && !resource.IsSystemKind(info.Kind)
		}},
		{"Privileged Tools (--system):", func(_ graph.NodeID, info graph.ResourceInfo) bool {
			return info.Privileged
		}},
		{"System:", func(_ graph.NodeID, info graph.ResourceInfo) bool {
			return resource.IsSystemKind(info.Kind)
		}},
	}

	// Lift (NodeID, ResourceInfo) predicates to the NodeID-only form
	// PrintTreeFiltered expects, looking the info up once per call.
	lift := func(pred func(graph.NodeID, graph.ResourceInfo) bool) func(graph.NodeID) bool {
		return func(id graph.NodeID) bool { return pred(id, result.resourceInfo[id]) }
	}

	// A section is non-empty if any resolver node passes its predicate.
	// Walking resolver.GetNodes() (not just result.resourceInfo) ensures
	// builtin Installers — which sit in the resolver but not in the info
	// map — count toward the "Tools:" section.
	nodes := result.resolver.GetNodes()
	rendered := 0
	for _, s := range sections {
		include := lift(s.include)
		empty := true
		for _, n := range nodes {
			if include(n.ID) {
				empty = false
				break
			}
		}
		if empty {
			continue
		}
		if rendered > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, s.heading)
		printer.PrintTreeFiltered(result.resolver, result.resourceInfo, include)
		rendered++
	}
}

// addDisabledResourceInfo injects disabled resources into the resource info map.
// Resources that already have an entry (e.g., ActionRemove for previously installed) are not overwritten.
func addDisabledResourceInfo(info map[graph.NodeID]graph.ResourceInfo, disabled []resource.Resource) {
	for _, res := range disabled {
		nodeID := graph.NewNodeID(res.Kind(), res.Name())
		// Do not overwrite existing entries (e.g., ActionRemove)
		if _, exists := info[nodeID]; exists {
			continue
		}
		ri := graph.ResourceInfo{
			Kind:       res.Kind(),
			Name:       res.Name(),
			Action:     resource.ActionSkip,
			Privileged: resource.IsPrivileged(res),
		}
		if tool, ok := res.(*resource.Tool); ok && tool.ToolSpec != nil {
			ri.Version = tool.ToolSpec.Version
		}
		info[nodeID] = ri
	}
}

// collectSkipInfos extracts ActionSkip entries from resourceInfo, sorted by kind then name.
func collectSkipInfos(resourceInfo map[graph.NodeID]graph.ResourceInfo) []graph.ResourceInfo {
	var infos []graph.ResourceInfo
	for _, info := range resourceInfo {
		if info.Action == resource.ActionSkip {
			infos = append(infos, info)
		}
	}
	slices.SortFunc(infos, func(a, b graph.ResourceInfo) int {
		if c := cmp.Compare(string(a.Kind), string(b.Kind)); c != 0 {
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return infos
}

// addSystemResourceInfo computes accurate actions for system resources
// using reconcilers and LoadReadOnly (no lock, no filesystem side effects)
// and merges them into the info map.
func addSystemResourceInfo(info map[graph.NodeID]graph.ResourceInfo, resources []resource.Resource, pathConfig *path.Paths) {
	// Check if system state directory exists before creating a store.
	// plan is a read-only command and should not create directories.
	systemDir := pathConfig.SystemDataDir()
	if _, err := os.Stat(systemDir); os.IsNotExist(err) {
		// No system state yet (first run) — use install fallback for supported
		// system installers and skip unsupported system resource kinds.
		markAllSystemAsInstall(info, resources)
		return
	}

	store, err := state.NewStore[state.SystemState](systemDir)
	if err != nil {
		slog.Warn("failed to create system state store for plan", "error", err)
		markAllSystemAsInstall(info, resources)
		return
	}

	// Use LoadReadOnly to avoid acquiring the state lock.
	// plan should have no filesystem side effects.
	st, err := store.LoadReadOnly()
	if err != nil {
		slog.Warn("failed to load system state for plan", "error", err)
		markAllSystemAsInstall(info, resources)
		return
	}

	// Extract system resources by kind
	var installers []*resource.SystemInstaller
	var repos []*resource.SystemPackageRepository
	var packages []*resource.SystemPackageSet
	for _, res := range resources {
		switch r := res.(type) {
		case *resource.SystemInstaller:
			installers = append(installers, r)
		case *resource.SystemPackageRepository:
			repos = append(repos, r)
		case *resource.SystemPackageSet:
			packages = append(packages, r)
		}
	}

	// Reconcile each resource type directly (reconcilers are stateless)
	installerActions := reconciler.NewSystemInstallerReconciler().Reconcile(installers, st.SystemInstallers)
	repoActions := reconciler.NewSystemPackageRepositoryReconciler().Reconcile(repos, st.SystemPackageRepositories)
	pkgActions := reconciler.NewSystemPackageSetReconciler().Reconcile(packages, st.SystemPackages)

	convertActions[*resource.SystemInstaller, *resource.SystemInstallerState](info, resource.KindSystemInstaller, installerActions)
	convertActions[*resource.SystemPackageRepository, *resource.SystemPackageRepositoryState](info, resource.KindSystemPackageRepository, repoActions)
	convertActions[*resource.SystemPackageSet, *resource.SystemPackageSetState](info, resource.KindSystemPackageSet, pkgActions)
}

// markAllSystemAsInstall marks all system resources as ActionInstall (first-run fallback).
// All system kinds (SystemInstaller, SystemPackageRepository, SystemPackageSet)
// have concrete installers as of #198, so every kind goes on the Install path.
func markAllSystemAsInstall(info map[graph.NodeID]graph.ResourceInfo, resources []resource.Resource) {
	for _, res := range resources {
		if resource.IsSystemKind(res.Kind()) {
			nodeID := graph.NewNodeID(res.Kind(), res.Name())
			info[nodeID] = graph.ResourceInfo{
				Kind:   res.Kind(),
				Name:   res.Name(),
				Action: resource.ActionInstall,
			}
		}
	}
}

// markPrivilegedAsSkip sets info.Action to ActionSkip when the resource is
// privileged and --system is not enabled. The predicate (resource.IsPrivileged)
// auto-extends to both Commands and download/registry-pattern privileged
// tools post-SUB4; the plan output mark is therefore consistent with the
// install-time skip in cmd/tomei/apply.go's filterPrivilegedWithLog.
func markPrivilegedAsSkip(info *graph.ResourceInfo, res resource.Resource, system bool) {
	if !system && resource.IsPrivileged(res) {
		info.Action = resource.ActionSkip
	}
}

// convertActions converts reconciler actions to graph.ResourceInfo entries.
func convertActions[R resource.Resource, S resource.State](
	info map[graph.NodeID]graph.ResourceInfo,
	kind resource.Kind,
	actions []reconciler.Action[R, S],
) {
	for _, action := range actions {
		nodeID := graph.NewNodeID(kind, action.Name)
		info[nodeID] = graph.ResourceInfo{
			Kind:   kind,
			Name:   action.Name,
			Action: action.Type,
		}
	}
}

// syncRegistryForPlan creates a store and syncs the aqua registry.
func syncRegistryForPlan(ctx context.Context) error {
	// Load config from fixed path (~/.config/tomei/config.cue)
	cfg, err := config.LoadConfig(config.DefaultConfigDir)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Setup paths from config
	pathConfig, err := path.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize paths: %w", err)
	}

	// Create state store
	store, err := state.NewStore[state.UserState](pathConfig.UserDataDir())
	if err != nil {
		return fmt.Errorf("failed to create state store: %w", err)
	}

	ghClient := github.NewHTTPClient(github.TokenFromEnv())
	return aqua.SyncRegistry(ctx, store, ghClient)
}
