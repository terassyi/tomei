package cue

import (
	"github.com/spf13/cobra"
)

var evalCmd = &cobra.Command{
	Use:   "eval <files or directories...>",
	Short: "Evaluate CUE manifests with tomei configuration",
	Long: `Evaluate CUE manifests with tomei's registry and tag configuration applied.

Unlike plain "cue eval", this command automatically:
  - Configures the OCI registry for tomei module resolution
  - Injects @tag() string values (os, arch, headless) for the current platform
  - Injects @if() boolean tags (darwin, linux, amd64, arm64, headless) so
    file-level conditional inclusion works without manual -t flags
  - Excludes config.cue from evaluation

Output is CUE text format (same as "cue eval").
See "tomei cue --help" for details on platform tag injection.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runEval,
}

func runEval(cmd *cobra.Command, args []string) error {
	return runCUEOutput(cmd, args, cueTextFormatter{})
}
