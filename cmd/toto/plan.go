package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/terassyi/toto/internal/config"
)

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Show the execution plan",
	Long: `Show the execution plan without applying changes.

Displays what actions would be taken:
  - install: New resources to install
  - upgrade: Resources to upgrade
  - reinstall: Resources to reinstall (due to taint)
  - remove: Resources to remove`,
	RunE: runPlan,
}

func runPlan(cmd *cobra.Command, _ []string) error {
	dir, err := getConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	cmd.Printf("Planning changes for %s\n", dir)

	loader := config.NewLoader(nil)
	resources, err := loader.Load(dir)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	cmd.Printf("Found %d resource(s)\n", len(resources))

	// TODO: Load state and diff
	// TODO: Build dependency graph
	// TODO: Show execution plan

	cmd.Println("Plan not yet fully implemented")
	return nil
}
