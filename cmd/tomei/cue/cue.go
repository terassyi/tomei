package cue

import "github.com/spf13/cobra"

// Cmd is the parent command for cue subcommands.
var Cmd = &cobra.Command{
	Use:   "cue",
	Short: "CUE module management commands",
	Long: `Commands for managing CUE module configuration.

Subcommands:
  init    Initialize a CUE module for tomei manifests`,
}

func init() {
	Cmd.AddCommand(initCmd)
}
