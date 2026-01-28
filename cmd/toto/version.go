package main

import "github.com/spf13/cobra"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version",
	Run: func(cmd *cobra.Command, _ []string) {
		cmd.Println("toto version", version)
	},
}
