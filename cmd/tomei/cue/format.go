package cue

import (
	"bytes"
	"encoding/json"
	"fmt"

	"cuelang.org/go/cue"
	"github.com/spf13/cobra"

	"github.com/terassyi/tomei/internal/config"
)

// Formatter formats a cue.Value for output.
type Formatter interface {
	Format(value cue.Value) (string, error)
}

// cueTextFormatter outputs CUE text (cue eval equivalent).
type cueTextFormatter struct{}

func (cueTextFormatter) Format(value cue.Value) (string, error) {
	return fmt.Sprint(value), nil
}

// jsonFormatter outputs indented JSON (cue export equivalent).
type jsonFormatter struct{}

func (jsonFormatter) Format(value cue.Value) (string, error) {
	jsonBytes, err := value.MarshalJSON()
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %w", err)
	}

	var indented bytes.Buffer
	if err := json.Indent(&indented, jsonBytes, "", "    "); err != nil {
		return "", fmt.Errorf("failed to indent JSON: %w", err)
	}

	return indented.String(), nil
}

// runCUEOutput is the shared implementation for eval and export commands.
func runCUEOutput(cmd *cobra.Command, args []string, formatter Formatter) error {
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

		s, err := formatter.Format(value)
		if err != nil {
			return err
		}

		fmt.Fprintln(out, s)
	}

	return nil
}
