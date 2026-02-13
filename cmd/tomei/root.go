package main

import (
	"github.com/spf13/cobra"
	cuecmd "github.com/terassyi/tomei/cmd/tomei/cue"
	statecmd "github.com/terassyi/tomei/cmd/tomei/state"
)

const outputJSON = "json"

var (
	systemMode bool
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
