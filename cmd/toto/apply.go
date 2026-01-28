package main

import "github.com/spf13/cobra"

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply the configuration",
	Long:  "Apply the configuration to install, upgrade, or remove resources.",
	RunE: func(_ *cobra.Command, _ []string) error {
		// TODO: implement
		return nil
	},
}
