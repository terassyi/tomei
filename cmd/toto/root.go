package main

import (
	"github.com/spf13/cobra"
	"github.com/terassyi/toto/internal/config"
	"github.com/terassyi/toto/internal/path"
)

var (
	systemMode bool
)

var rootCmd = &cobra.Command{
	Use:   "toto",
	Short: "Declarative development environment setup tool",
	Long: `Toto is a declarative development environment setup tool.
It manages tools, language runtimes, and system packages
using a Kubernetes-like Spec/State reconciliation pattern.

Commands are separated by privilege level:
  toto apply              Apply user-level resources (Runtime, Tool)
  sudo toto apply --system  Apply system-level resources (SystemPackage)`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().BoolVar(&systemMode, "system", false, "Apply system-level resources (requires root)")

	rootCmd.AddCommand(
		versionCmd,
		initCmd,
		applyCmd,
		validateCmd,
		planCmd,
		doctorCmd,
	)
}

// getConfigDir returns the fixed config directory path (~/.config/toto).
func getConfigDir() (string, error) {
	return path.Expand(config.DefaultConfigDir)
}
