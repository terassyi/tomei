package main

import "github.com/spf13/cobra"

var cueCmd = &cobra.Command{
	Use:   "cue",
	Short: "CUE module management commands",
	Long: `Commands for managing CUE module configuration.

Subcommands:
  init    Initialize a CUE module for tomei manifests`,
}

func init() {
	cueCmd.AddCommand(cueInitCmd)
}
