package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
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
	syncRegistry   bool
	updateTools    bool
	updateRuntimes bool
	updateAll      bool
	noColor        bool
	quiet          bool
	parallel       int
	yes            bool
	logLevel       string
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
	applyCmd.Flags().BoolVar(&applyCfg.updateTools, "update-tools", false, "Update tools with non-exact versions (latest + alias) to latest")
	applyCmd.Flags().BoolVar(&applyCfg.updateRuntimes, "update-runtimes", false, "Update runtimes with non-exact versions (latest + alias) to latest")
	applyCmd.Flags().BoolVar(&applyCfg.updateAll, "update-all", false, "Update all tools and runtimes with non-exact versions")
	applyCmd.Flags().BoolVar(&applyCfg.noColor, "no-color", false, "Disable colored output")
	applyCmd.Flags().BoolVar(&applyCfg.quiet, "quiet", false, "Suppress progress output")
	applyCmd.Flags().IntVar(&applyCfg.parallel, "parallel", engine.DefaultParallelism, "Maximum number of parallel installations (1-20)")
	applyCmd.Flags().BoolVarP(&applyCfg.yes, "yes", "y", false, "Skip confirmation prompt")
	applyCmd.Flags().StringVar(&applyCfg.logLevel, "log-level", "warn", "Log level for TUI panel (debug, info, warn, error)")
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

	// Setup paths from config
	pathConfig, err := path.NewFromConfig(appCfg)
	if err != nil {
		return fmt.Errorf("failed to initialize paths: %w", err)
	}

	// Check if tomei has been initialized
	stateFile := filepath.Join(pathConfig.UserDataDir(), "state.json")
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		return fmt.Errorf("tomei is not initialized. Run 'tomei init' first")
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

	// Sync registry if --sync flag is set, or if --update-tools/--update-all
	// is used (latest tools need latest registry for accurate resolution)
	if cfg.syncRegistry || cfg.updateTools || cfg.updateAll {
		if err := aqua.SyncRegistry(ctx, store, ghClient); err != nil {
			slog.Warn("failed to sync aqua registry", "error", err)
		}
	}

	// Show plan and ask for confirmation when there are changes
	updCfg := engine.UpdateConfig{
		SyncMode:       cfg.syncRegistry,
		UpdateTools:    cfg.updateTools || cfg.updateAll,
		UpdateRuntimes: cfg.updateRuntimes || cfg.updateAll,
	}
	hasChanges, err := planForResources(w, resources, cfg.noColor, updCfg)
	if err != nil {
		return fmt.Errorf("failed to plan: %w", err)
	}
	if hasChanges && !cfg.yes {
		fmt.Fprint(w, "\nDo you want to continue? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" { //nolint:goconst // simple confirmation pattern
			fmt.Fprintln(w, "Canceled.")
			return nil
		}
	}
	fmt.Fprintln(w)

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
	eng.SetUpdateConfig(updCfg)

	// Track results for summary
	results := &ui.ApplyResults{}

	// Create log store for capturing installation logs
	logsDir := pathConfig.UserCacheDir() + "/logs"
	logStore, err := tomeilog.NewStore(logsDir)
	if err != nil {
		slog.Warn("failed to create log store", "error", err)
	}
	if logStore != nil {
		defer logStore.Close()
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

	// Choose TUI or ProgressManager based on TTY
	isTTY := isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
	if isTTY && !cfg.quiet {
		return runApplyWithTUI(ctx, eng, resources, results, logStore, w, cfg)
	}
	return runApplyWithProgressManager(ctx, eng, resources, results, logStore, w, cfg)
}

// runApplyWithTUI runs apply with Bubble Tea TUI (for TTY mode).
func runApplyWithTUI(
	ctx context.Context,
	eng *engine.Engine,
	resources []resource.Resource,
	results *ui.ApplyResults,
	logStore *tomeilog.Store,
	w io.Writer,
	cfg *applyConfig,
) error {
	model := ui.NewApplyModel(results)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithOutput(w))

	// Route slog output into the TUI log panel instead of stderr
	logLevel := parseLogLevel(cfg.logLevel)
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(ui.NewTUILogHandler(p, logLevel)))
	defer slog.SetDefault(prevLogger)

	reporter := ui.NewThrottledReporter(p)

	// Set event handler: forward to reporter + log store
	eng.SetEventHandler(func(event engine.Event) {
		reporter.HandleEvent(event)
		if logStore != nil {
			handleLogEvent(logStore, event)
		}
	})

	// Run engine in background goroutine
	go func() {
		applyErr := eng.Apply(ctx, resources)
		reporter.Done(applyErr)
	}()

	// Run Bubble Tea in AltScreen (blocks until quit)
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	// AltScreen clears on exit, so reprint the final frame to scrollback
	fmt.Fprintln(w, model.FinalView())

	// Post-run: flush logs, print failures, print summary
	return finishApply(w, model.Err(), results, logStore, cfg)
}

// runApplyWithProgressManager runs apply with mpb-based progress bars (for non-TTY/quiet mode).
func runApplyWithProgressManager(
	ctx context.Context,
	eng *engine.Engine,
	resources []resource.Resource,
	results *ui.ApplyResults,
	logStore *tomeilog.Store,
	w io.Writer,
	cfg *applyConfig,
) error {
	// Apply log level filter for non-TUI mode
	logLevel := parseLogLevel(cfg.logLevel)
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))
	defer slog.SetDefault(prevLogger)

	pm := ui.NewProgressManager(w)
	defer pm.Wait()

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

	applyErr := eng.Apply(ctx, resources)
	return finishApply(w, applyErr, results, logStore, cfg)
}

// finishApply handles post-apply cleanup: flush logs, print failures, print summary.
func finishApply(w io.Writer, applyErr error, results *ui.ApplyResults, logStore *tomeilog.Store, cfg *applyConfig) error {
	if logStore != nil {
		if flushErr := logStore.Flush(); flushErr != nil {
			slog.Warn("failed to flush installation logs", "error", flushErr)
		}
		if cleanupErr := logStore.Cleanup(5); cleanupErr != nil {
			slog.Warn("failed to clean up old log sessions", "error", cleanupErr)
		}
	}

	if applyErr != nil {
		if logStore != nil && !cfg.quiet {
			ui.PrintFailureLogs(w, logStore.FailedResources())
		}
		if !cfg.quiet {
			ui.PrintApplySummary(w, results)
		}
		return fmt.Errorf("apply failed: %w", applyErr)
	}

	if !cfg.quiet {
		ui.PrintApplySummary(w, results)
	}
	return nil
}

// parseLogLevel converts a string log level to slog.Level.
// Accepted values: "debug", "info", "warn", "error" (case-insensitive).
// Defaults to slog.LevelWarn for unrecognized values.
func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelWarn
	}
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
