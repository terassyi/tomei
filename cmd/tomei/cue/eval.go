package cue

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/terassyi/tomei/internal/config"
)

var evalCmd = &cobra.Command{
	Use:   "eval <files or directories...>",
	Short: "Evaluate CUE manifests with tomei configuration",
	Long: `Evaluate CUE manifests with tomei's registry and @tag() configuration applied.

Unlike plain "cue eval", this command automatically:
  - Configures the OCI registry for tomei module resolution
  - Injects @tag() values (os, arch, headless) from the current platform
  - Excludes config.cue from evaluation

Output is CUE text format (same as "cue eval").`,
	Args: cobra.MinimumNArgs(1),
	RunE: runEval,
}

func runEval(cmd *cobra.Command, args []string) error {
	loader := config.NewLoader(nil)
	values, err := loader.EvalPaths(args)
	if err != nil {
		return fmt.Errorf("failed to evaluate: %w", err)
	}

	if len(values) == 0 {
		return fmt.Errorf("no CUE files found in the specified paths")
	}

	out := cmd.OutOrStdout()
	for i, value := range values {
		if i > 0 {
			fmt.Fprintln(out, "---")
		}
		fmt.Fprintln(out, value)
	}

	return nil
}
