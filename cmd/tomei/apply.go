package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/terassyi/tomei/internal/config"
	"github.com/terassyi/tomei/internal/github"
	"github.com/terassyi/tomei/internal/installer/download"
	"github.com/terassyi/tomei/internal/installer/engine"
	"github.com/terassyi/tomei/internal/installer/place"
	"github.com/terassyi/tomei/internal/installer/repository"
	"github.com/terassyi/tomei/internal/installer/runtime"
	"github.com/terassyi/tomei/internal/installer/tool"
	tomeilog "github.com/terassyi/tomei/internal/log"
	"github.com/terassyi/tomei/internal/path"
	"github.com/terassyi/tomei/internal/registry/aqua"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/state"
	"github.com/terassyi/tomei/internal/ui"
)

// applyConfig holds configuration for the apply command.
type applyConfig struct {
	syncRegistry bool
	noColor      bool
	quiet        bool
	parallel     int
}

var applyCfg applyConfig

var applyCmd = &cobra.Command{
	Use:   "apply <files or directories...>",
	Short: "Apply the configuration",
	Long: `Apply the configuration to install, upgrade, or remove resources.

For user-level resources (Runtime, Tool, ToolSet):
  tomei apply .
  tomei apply tools.cue runtime.cue
  tomei apply ~/.config/toto/

For system-level resources (SystemPackageRepository, SystemPackageSet):
  sudo tomei apply --system .`,
	Args: cobra.MinimumNArgs(1),
	RunE: runApply,
}

func init() {
	applyCmd.Flags().BoolVar(&applyCfg.syncRegistry, "sync", false, "Sync aqua registry to latest version before apply")
	applyCmd.Flags().BoolVar(&applyCfg.noColor, "no-color", false, "Disable colored output")
	applyCmd.Flags().BoolVar(&applyCfg.quiet, "quiet", false, "Suppress progress output")
	applyCmd.Flags().IntVar(&applyCfg.parallel, "parallel", engine.DefaultParallelism, "Maximum number of parallel installations (1-20)")
}

func runApply(cmd *cobra.Command, args []string) error {
	if applyCfg.noColor {
		color.NoColor = true
	}

	if systemMode {
		cmd.Printf("Applying system-level resources from %v\n", args)
		// TODO: implement system apply in Phase 4
		cmd.Println("System apply not yet implemented")
		return nil
	}

	cmd.Printf("Applying user-level resources from %v\n", args)
	return runUserApply(cmd.Context(), args, cmd.OutOrStdout(), &applyCfg)
}

func runUserApply(ctx context.Context, paths []string, w io.Writer, cfg *applyConfig) error {
	// Load resources from paths (manifests)
	loader := config.NewLoader(nil)
	resources, err := loader.LoadPaths(paths)
	if err != nil {
		return fmt.Errorf("failed to load resources: %w", err)
	}

	// Expand set resources (ToolSet, etc.) into individual resources
	resources, err = resource.ExpandSets(resources)
	if err != nil {
		return fmt.Errorf("failed to expand sets: %w", err)
	}

	// Load config from fixed path (~/.config/tomei/config.cue)
	appCfg, err := config.LoadConfig(config.DefaultConfigDir)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Sync schema.cue if it exists on disk
	if err := config.SyncSchema(appCfg, config.DefaultConfigDir); err != nil {
		return fmt.Errorf("failed to sync schema: %w", err)
	}

	// Setup paths from config
	pathConfig, err := path.NewFromConfig(appCfg)
	if err != nil {
		return fmt.Errorf("failed to initialize paths: %w", err)
	}

	// Ensure directories exist
	if err := path.EnsureDir(pathConfig.UserDataDir()); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}
	if err := path.EnsureDir(pathConfig.UserBinDir()); err != nil {
		return fmt.Errorf("failed to create bin directory: %w", err)
	}

	// Create state store
	store, err := state.NewStore[state.UserState](pathConfig.UserDataDir())
	if err != nil {
		return fmt.Errorf("failed to create state store: %w", err)
	}

	// Create GitHub-aware HTTP client (uses GITHUB_TOKEN/GH_TOKEN if available)
	ghClient := github.NewHTTPClient(github.TokenFromEnv())

	// Sync registry if --sync flag is set
	if cfg.syncRegistry {
		if err := aqua.SyncRegistry(ctx, store, ghClient); err != nil {
			slog.Warn("failed to sync aqua registry", "error", err)
		}
	}

	// Create installers
	downloader := download.NewDownloaderWithClient(ghClient)
	toolsDir := pathConfig.UserDataDir() + "/tools"
	runtimesDir := pathConfig.UserDataDir() + "/runtimes"
	binDir := pathConfig.UserBinDir()

	placer := place.NewPlacer(toolsDir, binDir)
	toolInstaller := tool.NewInstaller(downloader, placer)
	runtimeInstaller := runtime.NewInstaller(downloader, runtimesDir)
	reposDir := pathConfig.UserDataDir() + "/repositories"
	repoInstaller := repository.NewInstaller(reposDir)

	// Create engine with event handler for progress display
	eng := engine.NewEngine(toolInstaller, runtimeInstaller, repoInstaller, store)
	eng.SetParallelism(cfg.parallel)
	if cfg.syncRegistry {
		eng.SetSyncMode(true)
	}

	// Track results for summary
	results := &ui.ApplyResults{}

	// Create progress manager for download progress bars
	pm := ui.NewProgressManager(w)
	defer pm.Wait()

	// Create log store for capturing installation logs
	logsDir := pathConfig.UserCacheDir() + "/logs"
	logStore, err := tomeilog.NewStore(logsDir)
	if err != nil {
		slog.Warn("failed to create log store", "error", err)
	}

	// Set event handler for progress display and log capture
	if !cfg.quiet {
		eng.SetEventHandler(func(event engine.Event) {
			pm.HandleEvent(event, results)
			if logStore != nil {
				handleLogEvent(logStore, event)
			}
		})
	} else if logStore != nil {
		eng.SetEventHandler(func(event engine.Event) {
			handleLogEvent(logStore, event)
		})
	}

	// Set resolver configurer to be called after lock is acquired and state is loaded
	cacheDir := pathConfig.UserCacheDir() + "/registry/aqua"
	eng.SetResolverConfigurer(func(st *state.UserState) error {
		if st.Registry != nil && st.Registry.Aqua != nil {
			resolver := aqua.NewResolver(cacheDir, ghClient)
			toolInstaller.SetResolver(resolver, aqua.RegistryRef(st.Registry.Aqua.Ref))
			slog.Debug("configured aqua-registry resolver", "ref", st.Registry.Aqua.Ref)
		}
		return nil
	})

	// Run engine
	if err := eng.Apply(ctx, resources); err != nil {
		// Flush failed logs to disk and print failure details
		if logStore != nil {
			if flushErr := logStore.Flush(); flushErr != nil {
				slog.Warn("failed to flush installation logs", "error", flushErr)
			}
			if !cfg.quiet {
				ui.PrintFailureLogs(w, logStore.FailedResources())
			}
			if cleanupErr := logStore.Cleanup(5); cleanupErr != nil {
				slog.Warn("failed to clean up old log sessions", "error", cleanupErr)
			}
		}

		if !cfg.quiet {
			ui.PrintApplySummary(w, results)
		}

		return fmt.Errorf("apply failed: %w", err)
	}

	// Clean up old log sessions
	if logStore != nil {
		if cleanupErr := logStore.Cleanup(5); cleanupErr != nil {
			slog.Warn("failed to clean up old log sessions", "error", cleanupErr)
		}
	}

	// Print summary
	if !cfg.quiet {
		ui.PrintApplySummary(w, results)
	}

	return nil
}

// handleLogEvent dispatches an engine event to the LogStore.
func handleLogEvent(logStore *tomeilog.Store, event engine.Event) {
	switch event.Type {
	case engine.EventStart:
		logStore.RecordStart(event.Kind, event.Name, event.Version, string(event.Action), event.Method)
	case engine.EventOutput:
		logStore.RecordOutput(event.Kind, event.Name, event.Output)
	case engine.EventError:
		logStore.RecordError(event.Kind, event.Name, event.Error)
	case engine.EventComplete:
		logStore.RecordComplete(event.Kind, event.Name)
	}
}
