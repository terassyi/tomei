package main

import (
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose the environment",
	Long: `Diagnose the environment for potential issues.

Checks for:
  - Unmanaged tools in runtime bin paths (~/go/bin, ~/.cargo/bin)
  - Conflicts between toto-managed and unmanaged tools
  - State file integrity`,
	RunE: runDoctor,
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	cmd.Println("Running diagnostics...")

	// TODO: Check for unmanaged tools in GOBIN
	// TODO: Check for unmanaged tools in CARGO_HOME/bin
	// TODO: Check for conflicts in ~/.local/bin
	// TODO: Verify state file integrity

	cmd.Println("Doctor not yet implemented")
	return nil
}
