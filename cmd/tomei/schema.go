package main

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/terassyi/tomei/internal/config"
	"github.com/terassyi/tomei/internal/path"
)

var schemaCmd = &cobra.Command{
	Use:   "schema [directory]",
	Short: "Generate or update schema.cue",
	Long: `Generate or update schema.cue for CUE LSP support.

If directory is not specified, the current directory is used.
The schema.cue file provides type definitions and completion support
for CUE editors and language servers.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSchema,
}

func runSchema(_ *cobra.Command, args []string) error {
	dir := "."
	if len(args) > 0 {
		expanded, err := path.Expand(args[0])
		if err != nil {
			return fmt.Errorf("failed to expand directory path: %w", err)
		}
		dir = expanded
	}

	result, err := config.WriteSchema(dir)
	if err != nil {
		return err
	}

	schemaFile := filepath.Join(dir, config.SchemaFileName)
	switch result {
	case config.SchemaCreated:
		fmt.Printf("Created %s\n", schemaFile)
	case config.SchemaUpdated:
		fmt.Printf("Updated %s\n", schemaFile)
	case config.SchemaUpToDate:
		fmt.Printf("%s is already up to date\n", schemaFile)
	}
	return nil
}
