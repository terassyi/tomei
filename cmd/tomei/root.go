package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	cuecmd "github.com/terassyi/tomei/cmd/tomei/cue"
	statecmd "github.com/terassyi/tomei/cmd/tomei/state"
	"github.com/terassyi/tomei/internal/config"
	"github.com/terassyi/tomei/internal/verify"
)

const outputJSON = "json"

// logLevelFlag implements pflag.Value for slog.Level.
type logLevelFlag struct {
	level slog.Level
}

func (f *logLevelFlag) String() string { return strings.ToLower(f.level.String()) }
func (f *logLevelFlag) Type() string   { return "string" }
func (f *logLevelFlag) Set(s string) error {
	switch strings.ToLower(s) {
	case "debug":
		f.level = slog.LevelDebug
	case "info":
		f.level = slog.LevelInfo
	case "warn":
		f.level = slog.LevelWarn
	case "error":
		f.level = slog.LevelError
	default:
		return fmt.Errorf("unknown log level %q (valid: debug, info, warn, error)", s)
	}
	return nil
}

func (f *logLevelFlag) Level() slog.Level { return f.level }

var (
	systemMode     bool
	globalLogLevel = &logLevelFlag{level: slog.LevelWarn}
)

// loadConfig holds flags shared between apply and plan commands.
type loadConfig struct {
	syncRegistry   bool
	updateTools    bool
	updateRuntimes bool
	updateAll      bool
	noColor        bool
	ignoreCosign   bool
}

// registerFlags registers the common flags on the given command.
func (c *loadConfig) registerFlags(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&c.syncRegistry, "sync", false, "Sync aqua registry to latest version")
	cmd.Flags().BoolVar(&c.updateTools, "update-tools", false, "Update tools with non-exact versions (latest + alias) to latest")
	cmd.Flags().BoolVar(&c.updateRuntimes, "update-runtimes", false, "Update runtimes with non-exact versions (latest + alias) to latest")
	cmd.Flags().BoolVar(&c.updateAll, "update-all", false, "Update all tools and runtimes with non-exact versions")
	cmd.Flags().BoolVar(&c.noColor, "no-color", false, "Disable colored output")
	cmd.Flags().BoolVar(&c.ignoreCosign, "ignore-cosign", false, "Skip cosign signature verification for CUE module dependencies")
}

// verifierOpts returns LoaderOptions for cosign signature verification.
// If ignoreCosign is set or the verifier cannot be created, returns nil (no verification).
func (c *loadConfig) verifierOpts() []config.LoaderOption {
	if c.ignoreCosign {
		return nil
	}
	v, err := verify.NewSigstoreVerifier(config.CUERegistryOrDefault())
	if err != nil {
		slog.Warn("failed to create cosign verifier, skipping verification", "error", err)
		return nil
	}
	return []config.LoaderOption{config.WithVerifier(v)}
}

var rootCmd = &cobra.Command{
	Use:   "tomei",
	Short: "Declarative development environment setup tool",
	Long: `Tomei is a declarative development environment setup tool.
It manages tools, language runtimes, and system packages
using a Kubernetes-like Spec/State reconciliation pattern.

Commands are separated by privilege level:
  tomei apply              Apply user-level resources (Runtime, Tool)
  sudo tomei apply --system  Apply system-level resources (SystemPackage)`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: globalLogLevel.Level()})))
		return nil
	},
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().BoolVar(&systemMode, "system", false, "Apply system-level resources (requires root)")
	rootCmd.PersistentFlags().Var(globalLogLevel, "log-level", "Log level (debug, info, warn, error)")
	_ = rootCmd.RegisterFlagCompletionFunc("log-level", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"debug", "info", "warn", "error"}, cobra.ShellCompDirectiveNoFileComp
	})

	rootCmd.AddCommand(
		versionCmd,
		initCmd,
		uninitCmd,
		applyCmd,
		validateCmd,
		planCmd,
		doctorCmd,
		envCmd,
		logsCmd,
		getCmd,
		completionCmd,
		cuecmd.Cmd,
		statecmd.Cmd,
	)
}
