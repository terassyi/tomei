package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/terassyi/tomei/internal/config"
	"github.com/terassyi/tomei/internal/env"
	"github.com/terassyi/tomei/internal/path"
	"github.com/terassyi/tomei/internal/state"
)

var (
	envShell  string
	envExport bool
)

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Output environment variables for shell configuration",
	Long: `Output environment variable statements for installed runtimes.

Stdout mode (default):
  eval "$(tomei env)"

File export mode:
  tomei env --export
  source ~/.config/toto/env.sh

Shell types:
  --shell posix   POSIX-compatible (bash, zsh) [default]
  --shell fish    fish shell`,
	RunE: runEnv,
}

func init() {
	envCmd.Flags().StringVar(&envShell, "shell", "posix", "Shell type (posix, fish)")
	envCmd.Flags().BoolVar(&envExport, "export", false, "Write to file instead of stdout")
	_ = envCmd.RegisterFlagCompletionFunc("shell", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"posix", "fish"}, cobra.ShellCompDirectiveNoFileComp
	})
}

func runEnv(cmd *cobra.Command, _ []string) error {
	// Parse shell type
	shellType, err := env.ParseShellType(envShell)
	if err != nil {
		return err
	}

	// Load config
	cfg, err := config.LoadConfig(config.DefaultConfigDir)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Create paths
	paths, err := path.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create paths: %w", err)
	}

	// Load state (quiet: suppress warnings since stdout is eval'd by shell)
	store, err := state.NewStore[state.UserState](paths.UserDataDir())
	if err != nil {
		return fmt.Errorf("failed to create state store: %w", err)
	}
	store.SetQuiet(true)

	if err := store.Lock(); err != nil {
		return fmt.Errorf("failed to lock state: %w", err)
	}
	defer func() { _ = store.Unlock() }()

	userState, err := store.Load()
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	// Generate env output
	formatter := env.NewFormatter(shellType)
	lines := env.Generate(userState.Runtimes, paths.UserBinDir(), formatter)

	// Add CUE_REGISTRY if cue.mod/ exists in the working directory
	cwd, _ := os.Getwd()
	if cueRegistryLine := env.GenerateCUERegistry(
		hasCueMod(cwd),
		config.DefaultCUERegistry,
		formatter,
	); cueRegistryLine != "" {
		lines = append(lines, cueRegistryLine)
	}

	output := strings.Join(lines, "\n")
	if len(lines) > 0 {
		output += "\n"
	}

	// Export to file or print to stdout
	if envExport {
		return writeEnvFile(cmd, output, paths.EnvDir(), formatter.Ext())
	}

	fmt.Fprint(cmd.OutOrStdout(), output)
	return nil
}

// hasCueMod checks whether a cue.mod/ directory exists at or above dir.
func hasCueMod(dir string) bool {
	cur := dir
	for {
		if info, err := os.Stat(filepath.Join(cur, "cue.mod")); err == nil && info.IsDir() {
			return true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return false
}

func writeEnvFile(cmd *cobra.Command, content, envDir, ext string) error {
	if err := os.MkdirAll(envDir, 0755); err != nil {
		return fmt.Errorf("failed to create env dir: %w", err)
	}

	filePath := filepath.Join(envDir, "env"+ext)

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write env file: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s\n", filePath)
	return nil
}
