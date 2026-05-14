package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
	loadConfig
	quiet    bool
	parallel int
	yes      bool
	timeout  time.Duration
}

var applyCfg applyConfig

var applyCmd = &cobra.Command{
	Use:   "apply <files or directories...>",
	Short: "Apply the configuration",
	Long: `Apply the configuration to install, upgrade, or remove resources.

Tomei compares the desired state (CUE manifests) with the current state
(state.json in the data directory, default ~/.local/share/tomei/) and performs the minimal set of actions
to reconcile the difference. Running apply twice with unchanged manifests
produces no changes (idempotent).

For manifest writing guides (presets, platform tags, resource types),
see "tomei cue --help".

For user-level resources (Runtime, Tool, ToolSet):
  tomei apply .
  tomei apply tools.cue runtime.cue
  tomei apply ~/.config/tomei/

For privileged resources (tools with privileged: true):
  tomei apply --system .

For system resources (SystemInstaller, SystemPackageRepository, SystemPackageSet):
  tomei apply --system .

With --system, tomei prompts for sudo credentials once and keeps the
timestamp refreshed. Privileged tool commands and system package operations
run as the invoking user, using the cached sudo ticket without re-prompting
(subject to sudoers policy). Without --system, privileged and system
resources are skipped with a warning.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runApply,
}

func init() {
	applyCfg.registerFlags(applyCmd)
	applyCmd.Flags().BoolVar(&applyCfg.quiet, "quiet", false, "Suppress progress output")
	applyCmd.Flags().IntVar(&applyCfg.parallel, "parallel", engine.DefaultParallelism, "Maximum number of parallel installations (1-20)")
	applyCmd.Flags().BoolVarP(&applyCfg.yes, "yes", "y", false, "Skip confirmation prompt")
	applyCmd.Flags().DurationVar(&applyCfg.timeout, "timeout", download.DefaultDownloadTimeout, "Per-download timeout (e.g., 5m, 10m, 1h)")
}

func runApply(cmd *cobra.Command, args []string) error {
	if applyCfg.noColor {
		color.NoColor = true
	}

	if systemMode {
		cmd.Printf("Applying resources (including privileged and system) from %v\n", args)
	} else {
		cmd.Printf("Applying user-level resources from %v\n", args)
	}
	return executeApply(cmd.Context(), args, cmd.OutOrStdout(), &applyCfg, systemMode)
}

func executeApply(ctx context.Context, paths []string, w io.Writer, cfg *applyConfig, system bool) error {
	// Load resources from paths (manifests)
	loader := config.NewLoader(nil, cfg.verifierOpts()...)
	resources, err := loader.LoadPaths(paths)
	if err != nil {
		return fmt.Errorf("failed to load resources: %w", err)
	}

	// Expand set resources (ToolSet, etc.) into individual resources
	resources, err = resource.ExpandSets(resources)
	if err != nil {
		return fmt.Errorf("failed to expand sets: %w", err)
	}

	// Split resources into user-kind and system-kind
	userResources, systemResources := resource.FilterSystemKinds(resources)

	// Handle system resources: skip or prepare for execution
	var sysEng *engine.SystemEngine
	var supportedSystemResources []resource.Resource
	if !system && len(systemResources) > 0 {
		for _, r := range systemResources {
			slog.Info("skipping system resource (use --system)", "kind", r.Kind(), "name", r.Name())
		}
		fmt.Fprintf(w, "%d system resource(s) skipped. Use 'tomei apply --system' to manage.\n\n", len(systemResources))
	}
	if system && len(systemResources) > 0 {
		// Filter to resources with concrete installer implementations. As
		// of #198 all system kinds have installers; the helper is retained
		// so future system kinds can be staged through this channel.
		supported, skipped := filterSupportedSystemResources(systemResources)
		supportedSystemResources = supported
		if len(skipped) > 0 {
			for _, r := range skipped {
				slog.Info("skipping system resource (no installer wired)", "kind", r.Kind(), "name", r.Name())
			}
			fmt.Fprintf(w, "%d system resource(s) skipped (no installer wired for this kind).\n\n", len(skipped))
		}
	}

	// Filter out privileged resources when --system is not set
	if !system {
		normal, privileged := resource.FilterPrivileged(userResources)
		if len(privileged) > 0 {
			for _, r := range privileged {
				slog.Info("skipping privileged resource (use --system)", "kind", r.Kind(), "name", r.Name())
			}
			fmt.Fprintf(w, "%d privileged resource(s) skipped. Use 'tomei apply --system' to install.\n\n", len(privileged))
		}
		userResources = normal
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

	// Create GitHub-aware HTTP clients:
	// - ghClient: API client with Client.Timeout for registry sync, version resolution
	// - dlClient: download client with transport-level timeouts only (no Client.Timeout)
	//   to allow large binary downloads to complete at any speed
	token := github.TokenFromEnv()
	ghClient := github.NewHTTPClient(token)
	dlClient := &http.Client{
		Transport: github.WrapTransport(token, download.DefaultTransport()),
	}

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
	// Show plan with all resources (system + user) for complete picture
	hasChanges, err := planForResources(w, resources, cfg.noColor, updCfg, system)
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
	downloader := download.NewDownloaderWithClient(dlClient, download.WithDownloadTimeout(cfg.timeout))
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

	// Create SystemEngine when --system is set. Even with zero system resources
	// in the manifest, state may contain entries that need removal.
	if system {
		// systemDownloader is an un-authenticated downloader (no GitHub token
		// wrap) for SystemPackageRepository GPG-key fetches. It deliberately
		// does NOT share dlClient with the user-tier downloader: dlClient
		// wraps a GitHub PAT via github.WrapTransport, which host-scopes the
		// token to github.com / *.githubusercontent.com. A manifest pointing
		// keyUrl at a github-hosted URL would otherwise leak the PAT to the
		// request log of any GitHub repo the manifest author controls.
		// Vendor GPG keys (download.docker.com, dl.google.com, etc.) need
		// no GitHub auth.
		//
		// cfg.timeout here bounds only the GPG-key HTTP fetch. The apt-get
		// update invoked at the tail of the apt installer's Install runs via
		// command.Executor (shell-out), independent of this timeout — on a
		// slow mirror it may run longer than --timeout.
		systemDownloader := download.NewDownloader(download.WithDownloadTimeout(cfg.timeout))
		se, err := createSystemEngine(pathConfig.SystemDataDir(), systemDownloader)
		if err != nil {
			return fmt.Errorf("failed to create system engine: %w", err)
		}
		sysEng = se
	}

	// Acquire sudo credentials when --system is set (before TUI to allow interactive prompt).
	// We always acquire when --system is given, not only when the manifest contains
	// privileged tools, because privileged tools may exist only in state (removal case).
	if system {
		handler := &sudoHandler{}
		if err := handler.Acquire(ctx); err != nil {
			return err
		}
		defer func() {
			if err := handler.Release(); err != nil {
				slog.Warn("failed to invalidate sudo timestamp", "error", err)
			}
		}()
		eng.SetPrivilegeHandler(handler)
		if sysEng != nil {
			sysEng.SetPrivilegeHandler(handler)
		}
	}

	// Choose TUI or ProgressManager based on TTY
	isTTY := isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
	var applyErr error
	if isTTY && !cfg.quiet {
		applyErr = runApplyWithTUI(ctx, eng, userResources, sysEng, supportedSystemResources, results, logStore, w, cfg)
	} else {
		applyErr = runApplyWithProgressManager(ctx, eng, userResources, sysEng, supportedSystemResources, results, logStore, w, cfg)
	}

	// Report privileged action skips after apply completes.
	// When privileged manifest resources are excluded without --system, they
	// can otherwise be misrepresented here as removals (they're absent from
	// the desired resource list but still intended to be installed).
	if n := eng.SkippedPrivileged(); n > 0 && !cfg.quiet {
		fmt.Fprintf(w, "\n%d privileged resource action(s) skipped. Use 'tomei apply --system' to manage privileged resources.\n", n)
	}

	return applyErr
}

// runApplyWithTUI runs apply with Bubble Tea TUI (for TTY mode).
func runApplyWithTUI(
	ctx context.Context,
	eng *engine.Engine,
	userResources []resource.Resource,
	sysEng *engine.SystemEngine,
	systemResources []resource.Resource,
	results *ui.ApplyResults,
	logStore *tomeilog.Store,
	w io.Writer,
	cfg *applyConfig,
) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	model := ui.NewApplyModel(results)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithOutput(w))

	// Route slog output into the TUI log panel instead of stderr
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(ui.NewTUILogHandler(p, globalLogLevel.Level())))
	defer slog.SetDefault(prevLogger)

	reporter := ui.NewThrottledReporter(p)

	// Set event handler: forward to reporter + log store
	eventHandler := func(event engine.Event) {
		reporter.HandleEvent(event)
		if logStore != nil {
			handleLogEvent(logStore, event)
		}
	}
	eng.SetEventHandler(eventHandler)
	if sysEng != nil {
		sysEng.SetEventHandler(eventHandler)
	}

	// Run engines in background goroutine; signal completion via channel.
	// SystemEngine runs first (sequential), then Engine for user resources.
	engineDone := make(chan struct{})
	go func() {
		defer close(engineDone)
		if sysEng != nil {
			if err := sysEng.Apply(ctx, systemResources); err != nil {
				if ctx.Err() != nil {
					reporter.Done(ctx.Err())
					return
				}
				slog.Error("system resource apply failed, continuing with user resources", "error", err)
			}
		}
		applyErr := eng.Apply(ctx, userResources)
		reporter.Done(applyErr)
	}()

	// Run Bubble Tea in AltScreen (blocks until quit).
	// Note: Bubble Tea guarantees that Send() is safe to call after Run() returns —
	// once the program has terminated, Send() becomes a no-op.
	interrupted := false
	var tuiErr error
	if _, err := p.Run(); err != nil {
		if errors.Is(err, tea.ErrInterrupted) {
			interrupted = true
		} else {
			tuiErr = err
		}
	}
	if model.Interrupted() {
		interrupted = true
	}

	// Always cancel the engine context and wait for the goroutine to finish
	// before printing the summary or flushing logs. This ensures no concurrent
	// event emission during cleanup.
	cancel()
	<-engineDone

	if tuiErr != nil {
		return fmt.Errorf("TUI error: %w", tuiErr)
	}

	// AltScreen clears on exit, so reprint the final frame to scrollback
	fmt.Fprintln(w, model.FinalView())

	if interrupted {
		return finishApply(w, context.Canceled, results, logStore, cfg)
	}

	// Post-run: flush logs, print failures, print summary
	return finishApply(w, model.Err(), results, logStore, cfg)
}

// runApplyWithProgressManager runs apply with mpb-based progress bars (for non-TTY/quiet mode).
func runApplyWithProgressManager(
	ctx context.Context,
	eng *engine.Engine,
	userResources []resource.Resource,
	sysEng *engine.SystemEngine,
	systemResources []resource.Resource,
	results *ui.ApplyResults,
	logStore *tomeilog.Store,
	w io.Writer,
	cfg *applyConfig,
) error {
	// Apply log level filter for non-TUI mode
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: globalLogLevel.Level()})))
	defer slog.SetDefault(prevLogger)

	pm := ui.NewProgressManager(w)
	defer pm.Wait()

	// Set event handler for progress display and log capture
	eventHandler := func(event engine.Event) {
		if !cfg.quiet {
			pm.HandleEvent(event, results)
		}
		if logStore != nil {
			handleLogEvent(logStore, event)
		}
	}
	eng.SetEventHandler(eventHandler)
	if sysEng != nil {
		sysEng.SetEventHandler(eventHandler)
	}

	// Run system engine first, then user engine
	if sysEng != nil {
		if err := sysEng.Apply(ctx, systemResources); err != nil {
			if ctx.Err() != nil {
				return finishApply(w, ctx.Err(), results, logStore, cfg)
			}
			slog.Error("system resource apply failed, continuing with user resources", "error", err)
			fmt.Fprintf(w, "Warning: system resource apply failed: %v\n\n", err)
		}
	}

	applyErr := eng.Apply(ctx, userResources)
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
		if errors.Is(applyErr, context.Canceled) {
			return context.Canceled
		}
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

// sudoHandler implements engine.PrivilegeHandler for managing sudo session lifecycle.
// It validates credentials interactively, probes non-interactive access,
// and runs a background keepalive to prevent timeout during long installations.
type sudoHandler struct {
	cancel context.CancelFunc
	once   sync.Once
}

// Acquire validates sudo credentials and starts a keepalive goroutine.
func (h *sudoHandler) Acquire(ctx context.Context) error {
	// Probe first: if cached credentials or passwordless sudo already work,
	// avoid an unnecessary interactive prompt. This keeps CI / non-TTY flows
	// working when sudoers is configured for NOPASSWD.
	if err := exec.CommandContext(ctx, "sudo", "-n", "true").Run(); err != nil {
		stdinFD := os.Stdin.Fd()
		if !isatty.IsTerminal(stdinFD) && !isatty.IsCygwinTerminal(stdinFD) {
			return fmt.Errorf("sudo requires interactive authentication, but stdin is not a TTY; rerun interactively or configure passwordless sudo for --system mode: %w", err)
		}

		// Interactive sudo -v (prompts for password if needed).
		cmd := exec.CommandContext(ctx, "sudo", "-v")
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("sudo authentication failed: %w", err)
		}

		// Verify that non-interactive sudo now works as required by the
		// keepalive loop and later privileged commands.
		if err := exec.CommandContext(ctx, "sudo", "-n", "true").Run(); err != nil {
			return fmt.Errorf("sudo -n failed after sudo -v; your system may have tty_tickets enabled or a restrictive sudoers policy: %w", err)
		}
	}

	// Background keepalive: refresh sudo timestamp every 45 seconds.
	// Uses -n to avoid blocking on a password prompt if the timestamp expires
	// between ticks (e.g., system sleep/resume).
	keepCtx, cancel := context.WithCancel(ctx)
	h.cancel = cancel
	go func() {
		ticker := time.NewTicker(45 * time.Second)
		defer ticker.Stop()
		failures := 0
		for {
			select {
			case <-keepCtx.Done():
				return
			case <-ticker.C:
				if err := exec.CommandContext(keepCtx, "sudo", "-n", "-v").Run(); err != nil {
					failures++
					slog.Warn("sudo keepalive failed", "consecutive_failures", failures, "error", err)
					if failures >= 2 {
						slog.Error("sudo keepalive failed repeatedly, sudo session may have expired")
						return
					}
				} else {
					failures = 0
				}
			}
		}
	}()

	return nil
}

// Release invalidates the sudo timestamp and stops the keepalive goroutine.
// Safe to call multiple times (idempotent via sync.Once).
func (h *sudoHandler) Release() error {
	var err error
	h.once.Do(func() {
		if h.cancel != nil {
			h.cancel()
		}
		// Use a short timeout to avoid hanging if sudo is unresponsive.
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err = exec.CommandContext(releaseCtx, "sudo", "-k").Run()
	})
	return err
}
