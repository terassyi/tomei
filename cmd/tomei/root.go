package main

import (
	"log/slog"

	"github.com/spf13/cobra"

	cuecmd "github.com/terassyi/tomei/cmd/tomei/cue"
	statecmd "github.com/terassyi/tomei/cmd/tomei/state"
	"github.com/terassyi/tomei/internal/config"
	"github.com/terassyi/tomei/internal/verify"
)

const outputJSON = "json"

var (
	systemMode   bool
	ignoreCosign bool
)

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
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().BoolVar(&systemMode, "system", false, "Apply system-level resources (requires root)")
	rootCmd.PersistentFlags().BoolVar(&ignoreCosign, "ignore-cosign", false, "Skip cosign signature verification for CUE module dependencies")

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

// buildVerifierOpts returns LoaderOptions for cosign signature verification.
// If --ignore-cosign is set or the verifier cannot be created, returns nil (no verification).
func buildVerifierOpts() []config.LoaderOption {
	if ignoreCosign {
		return nil
	}
	v, err := verify.NewSigstoreVerifier(config.CUERegistryOrDefault())
	if err != nil {
		slog.Warn("failed to create cosign verifier, skipping verification", "error", err)
		return nil
	}
	return []config.LoaderOption{config.WithVerifier(v)}
}
