package main

import "github.com/spf13/cobra"

var rootCmd = &cobra.Command{
	Use:   "toto",
	Short: "Declarative development environment setup tool",
	Long: `Toto is a declarative development environment setup tool.
It manages tools, language runtimes, and system packages
using a Kubernetes-like Spec/State reconciliation pattern.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.AddCommand(
		versionCmd,
		applyCmd,
		diffCmd,
	)
}
