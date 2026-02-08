package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/terassyi/tomei/internal/config"
	"github.com/terassyi/tomei/internal/path"
	"github.com/terassyi/tomei/internal/ui"
)

var uninitCmd = &cobra.Command{
	Use:   "uninit",
	Short: "Remove tomei directories and state",
	Long: `Remove tomei directories, state files, and managed symlinks.

This command removes:
  - Config directory (~/.config/toto/)
  - Data directory (~/.local/share/tomei/)
  - Symlinks in bin directory (~/.local/bin/) that point to tomei-managed tools

The bin directory itself is preserved as it may contain non-toto files.

Use --keep-config to preserve the config directory.
Use --dry-run to see what would be removed without actually removing.`,
	RunE: runUninit,
}

var (
	uninitYes        bool
	uninitKeepConfig bool
	uninitDryRun     bool
	uninitNoColor    bool
)

func init() {
	uninitCmd.Flags().BoolVarP(&uninitYes, "yes", "y", false, "Skip confirmation prompt")
	uninitCmd.Flags().BoolVar(&uninitKeepConfig, "keep-config", false, "Preserve config directory")
	uninitCmd.Flags().BoolVar(&uninitDryRun, "dry-run", false, "Show what would be removed without removing")
	uninitCmd.Flags().BoolVar(&uninitNoColor, "no-color", false, "Disable color output")
}

// uninitContext holds the state for uninit operation.
type uninitContext struct {
	w          io.Writer
	errW       io.Writer
	style      *ui.Style
	cfgDir     string
	dataDir    string
	binDir     string
	symlinks   []string
	keepConfig bool
	dryRun     bool
	yes        bool
}

func runUninit(cmd *cobra.Command, _ []string) error {
	if uninitNoColor {
		color.NoColor = true
	}

	ctx, err := newUninitContext(cmd)
	if err != nil {
		return err
	}

	if ctx == nil {
		// Not initialized
		cmd.Println("tomei is not initialized. Nothing to remove.")
		return nil
	}

	ctx.printHeader()
	ctx.printTargets()

	if ctx.dryRun {
		fmt.Fprintln(ctx.w, "No changes made.")
		return nil
	}

	if !ctx.confirm() {
		fmt.Fprintln(ctx.w, "Aborted.")
		return nil
	}

	ctx.remove()
	ctx.printFooter()

	return nil
}

func newUninitContext(cmd *cobra.Command) (*uninitContext, error) {
	cfgDir, err := path.Expand(config.DefaultConfigDir)
	if err != nil {
		return nil, fmt.Errorf("failed to expand config directory: %w", err)
	}

	paths, err := path.New()
	if err != nil {
		return nil, fmt.Errorf("failed to get paths: %w", err)
	}

	// Check if toto is initialized
	if _, err := os.Stat(paths.UserStateFile()); os.IsNotExist(err) {
		return nil, nil
	}

	// Load config to get actual paths
	cfg, err := config.LoadConfig(cfgDir)
	if err != nil {
		cfg = config.DefaultConfig()
	}

	paths, err = path.NewFromConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create paths from config: %w", err)
	}

	dataDir := paths.UserDataDir()
	binDir := paths.UserBinDir()

	symlinks, err := findManagedSymlinks(binDir, dataDir)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to scan symlinks: %v\n", err)
		symlinks = nil
	}

	return &uninitContext{
		w:          cmd.OutOrStdout(),
		errW:       cmd.ErrOrStderr(),
		style:      ui.NewStyle(),
		cfgDir:     cfgDir,
		dataDir:    dataDir,
		binDir:     binDir,
		symlinks:   symlinks,
		keepConfig: uninitKeepConfig,
		dryRun:     uninitDryRun,
		yes:        uninitYes,
	}, nil
}

func (c *uninitContext) printHeader() {
	if c.dryRun {
		fmt.Fprintln(c.w, "Uninitializing toto... (dry-run)")
	} else {
		fmt.Fprintln(c.w, "Uninitializing toto...")
	}
	fmt.Fprintln(c.w)
}

func (c *uninitContext) printTargets() {
	header := "This will remove:"
	if c.dryRun {
		header = "Would remove:"
	}
	c.style.Header.Fprintln(c.w, header)

	for _, link := range c.symlinks {
		target, _ := os.Readlink(link)
		fmt.Fprintf(c.w, "  %s -> %s\n", c.style.Path.Sprint(link), target)
	}
	fmt.Fprintf(c.w, "  %s\n", c.style.Path.Sprint(c.dataDir))
	if !c.keepConfig {
		fmt.Fprintf(c.w, "  %s\n", c.style.Path.Sprint(c.cfgDir))
	}
	fmt.Fprintln(c.w)

	if c.keepConfig {
		c.style.Header.Fprintln(c.w, "Config will be preserved:")
		fmt.Fprintf(c.w, "  %s\n", c.style.Path.Sprint(c.cfgDir))
		fmt.Fprintln(c.w)
	}
}

func (c *uninitContext) confirm() bool {
	if c.yes {
		return true
	}

	fmt.Fprint(c.w, "Proceed? [y/N]: ")
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "y" || answer == "yes"
}

func (c *uninitContext) remove() {
	fmt.Fprintln(c.w)
	c.style.Header.Fprintln(c.w, "Removing:")

	// Remove symlinks first
	for _, link := range c.symlinks {
		target, _ := os.Readlink(link)
		c.removeItem(link, target)
	}

	// Remove data directory
	c.removeItem(c.dataDir, "")

	// Remove config directory (unless --keep-config)
	if !c.keepConfig {
		c.removeItem(c.cfgDir, "")
	}
}

func (c *uninitContext) removeItem(path, target string) {
	var err error
	if target != "" {
		// Symlink
		err = os.Remove(path)
	} else {
		// Directory
		err = os.RemoveAll(path)
	}

	if err != nil {
		if target != "" {
			fmt.Fprintf(c.w, "  %s %s -> %s (%v)\n", c.style.FailMark, path, target, err)
		} else {
			fmt.Fprintf(c.w, "  %s %s (%v)\n", c.style.FailMark, path, err)
		}
	} else {
		if target != "" {
			fmt.Fprintf(c.w, "  %s %s -> %s\n", c.style.SuccessMark, c.style.Path.Sprint(path), target)
		} else {
			fmt.Fprintf(c.w, "  %s %s\n", c.style.SuccessMark, c.style.Path.Sprint(path))
		}
	}
}

func (c *uninitContext) printFooter() {
	fmt.Fprintln(c.w)
	c.style.Success.Fprintln(c.w, "Uninitialization complete!")
	fmt.Fprintln(c.w)
	fmt.Fprintf(c.w, "Note: %s directory was preserved.\n", c.style.Path.Sprint(c.binDir))
}

// findManagedSymlinks finds symlinks in binDir that point to files under dataDir.
func findManagedSymlinks(binDir, dataDir string) ([]string, error) {
	entries, err := os.ReadDir(binDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var symlinks []string
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink == 0 {
			continue
		}

		linkPath := filepath.Join(binDir, entry.Name())
		target, err := os.Readlink(linkPath)
		if err != nil {
			continue
		}

		// Resolve relative symlinks
		if !filepath.IsAbs(target) {
			target = filepath.Join(binDir, target)
		}

		// Check if target points to tomei-managed directory
		if strings.HasPrefix(target, dataDir) {
			symlinks = append(symlinks, linkPath)
		}
	}

	return symlinks, nil
}
