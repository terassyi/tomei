package main

import "github.com/spf13/cobra"

var diffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Show the difference between Spec and State",
	Long:  "Show what actions would be performed without actually executing them.",
	RunE: func(_ *cobra.Command, _ []string) error {
		// TODO: implement
		return nil
	},
}
