package state

import "github.com/spf13/cobra"

// Cmd is the parent command for state subcommands.
var Cmd = &cobra.Command{
	Use:   "state",
	Short: "Manage state",
	Long:  "Commands for inspecting and managing tomei state.",
}

func init() {
	Cmd.AddCommand(diffCmd)
}
