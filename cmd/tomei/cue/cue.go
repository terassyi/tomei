package cue

import "github.com/spf13/cobra"

// Cmd is the parent command for cue subcommands.
var Cmd = &cobra.Command{
	Use:   "cue",
	Short: "CUE module management commands",
	Long: `Commands for managing CUE module configuration.

Subcommands:
  init       Initialize a CUE module for tomei manifests
  scaffold   Generate a CUE manifest scaffold for a resource kind
  eval       Evaluate CUE manifests with tomei configuration
  export     Export CUE manifests as JSON with tomei configuration`,
}

func init() {
	Cmd.AddCommand(initCmd)
	Cmd.AddCommand(scaffoldCmd)
	Cmd.AddCommand(evalCmd)
	Cmd.AddCommand(exportCmd)
}
