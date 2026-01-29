package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply the configuration",
	Long: `Apply the configuration to install, upgrade, or remove resources.

For user-level resources (Runtime, Tool, ToolSet):
  toto apply

For system-level resources (SystemPackageRepository, SystemPackageSet):
  sudo toto apply --system

It is recommended to run system-level apply first, then user-level.`,
	RunE: runApply,
}

func runApply(cmd *cobra.Command, _ []string) error {
	dir, err := getConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	if systemMode {
		cmd.Printf("Applying system-level resources from %s\n", dir)
		// TODO: implement system apply
		cmd.Println("System apply not yet implemented")
	} else {
		cmd.Printf("Applying user-level resources from %s\n", dir)
		// TODO: implement user apply
		cmd.Println("User apply not yet implemented")
	}

	return nil
}
