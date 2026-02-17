package cue

import (
	"github.com/spf13/cobra"
)

var exportCmd = &cobra.Command{
	Use:   "export <files or directories...>",
	Short: "Export CUE manifests as JSON with tomei configuration",
	Long: `Export CUE manifests as JSON with tomei's registry and @tag() configuration applied.

Unlike plain "cue export", this command automatically:
  - Configures the OCI registry for tomei module resolution
  - Injects @tag() values (os, arch, headless) from the current platform
  - Excludes config.cue from evaluation

Output is indented JSON.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runExport,
}

func runExport(cmd *cobra.Command, args []string) error {
	return runCUEOutput(cmd, args, jsonFormatter{})
}
