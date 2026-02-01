package main

import (
	"github.com/spf13/cobra"
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
