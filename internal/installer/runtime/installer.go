package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/terassyi/tomei/internal/checksum"
	"github.com/terassyi/tomei/internal/installer/command"
	"github.com/terassyi/tomei/internal/installer/download"
	"github.com/terassyi/tomei/internal/installer/extract"
	"github.com/terassyi/tomei/internal/path"
	"github.com/terassyi/tomei/internal/resource"
)

// CommandRunner is the interface for executing shell commands.
// This enables testing with mocks instead of real command execution.
type CommandRunner interface {
	ExecuteWithEnv(ctx context.Context, cmds []string, vars command.Vars, env map[string]string) error
	ExecuteWithOutput(ctx context.Context, cmds []string, vars command.Vars, env map[string]string, callback command.OutputCallback) error
	ExecuteCapture(ctx context.Context, cmds []string, vars command.Vars, env map[string]string) (string, error)
	Check(ctx context.Context, cmds []string, vars command.Vars, env map[string]string) bool
}

// Installer installs runtimes using the download or delegation pattern.
type Installer struct {
	downloader       download.Downloader
	cmdExecutor      CommandRunner
	runtimesDir      string
	progressCallback download.ProgressCallback
}

// NewInstaller creates a new runtime Installer.
func NewInstaller(downloader download.Downloader, runtimesDir string) *Installer {
	return &Installer{
		downloader:  downloader,
		cmdExecutor: command.NewExecutor(""),
		runtimesDir: runtimesDir,
	}
}

// NewInstallerWithRunner creates a new runtime Installer with a custom CommandRunner (for testing).
func NewInstallerWithRunner(downloader download.Downloader, runtimesDir string, runner CommandRunner) *Installer {
	return &Installer{
		downloader:  downloader,
		cmdExecutor: runner,
		runtimesDir: runtimesDir,
	}
}

// SetProgressCallback sets a callback for download progress.
func (i *Installer) SetProgressCallback(callback download.ProgressCallback) {
	i.progressCallback = callback
}

// Install installs a runtime according to the resource and returns its state.
// Supports both download and delegation types.
func (i *Installer) Install(ctx context.Context, res *resource.Runtime, name string) (*resource.RuntimeState, error) {
	spec := res.RuntimeSpec

	slog.Debug("installing runtime", "name", name, "version", spec.Version, "type", spec.Type)

	switch spec.Type {
	case resource.InstallTypeDownload:
		return i.installDownload(ctx, spec, name)
	case resource.InstallTypeDelegation:
		return i.installDelegation(ctx, spec, name)
	default:
		return nil, fmt.Errorf("unsupported type: %s", spec.Type)
	}
}

// installDownload installs a runtime using the download pattern.
func (i *Installer) installDownload(ctx context.Context, spec *resource.RuntimeSpec, name string) (*resource.RuntimeState, error) {
	// Validate spec
	if spec.Source == nil || spec.Source.URL == "" {
		return nil, fmt.Errorf("source.url is required for download pattern")
	}

	// Calculate install path
	installPath := filepath.Join(i.runtimesDir, name, spec.Version)

	// Check if already installed
	if _, err := os.Stat(installPath); err == nil {
		slog.Debug("runtime already installed, skipping", "name", name, "version", spec.Version)
		binDir, err := i.resolveBinDir(spec)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve bin directory: %w", err)
		}
		return i.buildState(spec, installPath, binDir), nil
	}

	// Download
	tmpDir, err := os.MkdirTemp("", "tomei-runtime-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	urlFilename := filepath.Base(spec.Source.URL)
	archivePath := filepath.Join(tmpDir, urlFilename)
	// Prefer context callback for parallel execution
	progressCb := download.CallbackFromContext[download.ProgressCallback](ctx)
	if progressCb == nil {
		progressCb = i.progressCallback
	}
	_, err = i.downloader.DownloadWithProgress(ctx, spec.Source.URL, archivePath, progressCb)
	if err != nil {
		return nil, fmt.Errorf("failed to download: %w", err)
	}

	// Verify checksum
	if err := i.downloader.Verify(ctx, archivePath, spec.Source.Checksum); err != nil {
		return nil, fmt.Errorf("failed to verify checksum: %w", err)
	}

	// Determine archive type
	archiveType := spec.Source.ArchiveType
	if archiveType == "" {
		archiveType = extract.DetectArchiveType(spec.Source.URL)
		if archiveType == "" {
			return nil, fmt.Errorf("cannot determine archive type from URL: %s", spec.Source.URL)
		}
		slog.Debug("auto-detected archive type", "type", archiveType)
	}

	// Extract
	extractor, err := extract.NewExtractor(archiveType)
	if err != nil {
		return nil, fmt.Errorf("failed to create extractor: %w", err)
	}

	extractDir := filepath.Join(tmpDir, "extracted")
	archiveFile, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open archive: %w", err)
	}
	defer archiveFile.Close()

	if err := extractor.Extract(archiveFile, extractDir); err != nil {
		return nil, fmt.Errorf("failed to extract: %w", err)
	}

	// Find the root directory in extracted content
	// Many runtimes have a top-level directory (e.g., go1.25.1.linux-amd64 extracts to "go/")
	rootDir, err := findExtractedRoot(extractDir)
	if err != nil {
		return nil, fmt.Errorf("failed to find extracted root: %w", err)
	}

	// Move to install path
	if err := os.MkdirAll(filepath.Dir(installPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create install directory: %w", err)
	}

	if err := os.Rename(rootDir, installPath); err != nil {
		// Rename may fail across filesystems, try copy
		if err := copyDir(rootDir, installPath); err != nil {
			return nil, fmt.Errorf("failed to move runtime to install path: %w", err)
		}
	}

	// Create symlinks for binaries
	binDir, err := i.resolveBinDir(spec)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve bin directory: %w", err)
	}
	if binDir != "" {
		if err := i.createSymlinks(installPath, spec.Binaries, binDir); err != nil {
			return nil, fmt.Errorf("failed to create symlinks: %w", err)
		}
	}

	slog.Debug("runtime installed successfully", "name", name, "version", spec.Version, "path", installPath)

	return i.buildState(spec, installPath, binDir), nil
}

// Remove removes an installed runtime.
func (i *Installer) Remove(ctx context.Context, st *resource.RuntimeState, name string) error {
	slog.Debug("removing runtime", "name", name, "version", st.Version, "type", st.Type)

	if st.Type.IsDelegation() {
		return i.removeDelegation(ctx, st, name)
	}

	// Download pattern: remove symlinks and install directory
	if st.BinDir != "" {
		for _, binary := range st.Binaries {
			linkPath := filepath.Join(st.BinDir, binary)
			if err := os.Remove(linkPath); err != nil && !os.IsNotExist(err) {
				slog.Debug("failed to remove symlink", "path", linkPath, "error", err)
			}
		}
	}

	if st.InstallPath != "" {
		if err := os.RemoveAll(st.InstallPath); err != nil {
			return fmt.Errorf("failed to remove install directory: %w", err)
		}
		// Try to remove version directory if empty
		versionDir := filepath.Dir(st.InstallPath)
		_ = os.Remove(versionDir)
	}

	slog.Debug("runtime removed", "name", name)
	return nil
}

// removeDelegation removes a delegation-pattern runtime by executing its remove command.
func (i *Installer) removeDelegation(ctx context.Context, st *resource.RuntimeState, name string) error {
	if len(st.RemoveCommand) == 0 {
		slog.Warn("no remove command for delegation runtime, skipping", "name", name)
		return nil
	}

	if err := i.cmdExecutor.ExecuteWithEnv(ctx, st.RemoveCommand, command.Vars{}, st.Env); err != nil {
		return fmt.Errorf("bootstrap remove failed: %w", err)
	}

	slog.Debug("delegation runtime removed", "name", name)
	return nil
}

// buildState creates a RuntimeState from the installation.
func (i *Installer) buildState(spec *resource.RuntimeSpec, installPath, binDir string) *resource.RuntimeState {
	digest := ""
	if spec.Source != nil && spec.Source.Checksum != nil {
		digest = checksum.ExtractHash(spec.Source.Checksum)
	}

	// Expand ~ in toolBinPath
	toolBinPath := spec.ToolBinPath
	if expanded, err := path.Expand(toolBinPath); err == nil {
		toolBinPath = expanded
	}

	// Expand ~ in env values
	env := make(map[string]string)
	for k, v := range spec.Env {
		if expanded, err := path.Expand(v); err == nil {
			env[k] = expanded
		} else {
			env[k] = v
		}
	}

	return &resource.RuntimeState{
		Type:           spec.Type,
		Version:        spec.Version,
		VersionKind:    resource.ClassifyVersion(spec.Version),
		SpecVersion:    spec.Version,
		Digest:         digest,
		InstallPath:    installPath,
		Binaries:       spec.Binaries,
		BinDir:         binDir,
		ToolBinPath:    toolBinPath,
		Commands:       spec.Commands,
		Env:            env,
		TaintOnUpgrade: spec.TaintOnUpgrade,
		UpdatedAt:      time.Now(),
	}
}

// installDelegation installs a runtime using the delegation pattern.
func (i *Installer) installDelegation(ctx context.Context, spec *resource.RuntimeSpec, name string) (*resource.RuntimeState, error) {
	// Validate spec
	if spec.Bootstrap == nil {
		return nil, fmt.Errorf("bootstrap is required for delegation pattern")
	}
	if len(spec.Bootstrap.Install) == 0 {
		return nil, fmt.Errorf("bootstrap.install is required for delegation pattern")
	}

	resolvedVersion := spec.Version
	versionKind := resource.ClassifyVersion(spec.Version)

	// Resolve version alias if configured
	if len(spec.Bootstrap.ResolveVersion) > 0 {
		slog.Debug("resolving version alias", "name", name, "alias", spec.Version)
		resolved, err := i.cmdExecutor.ExecuteCapture(ctx, spec.Bootstrap.ResolveVersion, command.Vars{Version: spec.Version}, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve version: %w", err)
		}
		if resolved != "" {
			slog.Debug("version resolved", "name", name, "alias", spec.Version, "resolved", resolved)
			resolvedVersion = resolved
			versionKind = resource.VersionAlias
		}
	}

	// Prepare environment
	env := make(map[string]string)
	for k, v := range spec.Env {
		if expanded, err := path.Expand(v); err == nil {
			env[k] = expanded
		} else {
			env[k] = v
		}
	}

	// Execute bootstrap install command
	vars := command.Vars{Version: resolvedVersion}
	outputCb := download.CallbackFromContext[download.OutputCallback](ctx)
	if outputCb != nil {
		if err := i.cmdExecutor.ExecuteWithOutput(ctx, spec.Bootstrap.Install, vars, env, command.OutputCallback(outputCb)); err != nil {
			return nil, fmt.Errorf("bootstrap install failed: %w", err)
		}
	} else {
		if err := i.cmdExecutor.ExecuteWithEnv(ctx, spec.Bootstrap.Install, vars, env); err != nil {
			return nil, fmt.Errorf("bootstrap install failed: %w", err)
		}
	}

	// Verify installation with check command
	if len(spec.Bootstrap.Check) > 0 {
		if !i.cmdExecutor.Check(ctx, spec.Bootstrap.Check, command.Vars{}, env) {
			return nil, fmt.Errorf("bootstrap check failed after install")
		}
	}

	// Expand paths
	toolBinPath := spec.ToolBinPath
	if expanded, err := path.Expand(toolBinPath); err == nil {
		toolBinPath = expanded
	}

	binDir := spec.BinDir
	if binDir != "" {
		if expanded, err := path.Expand(binDir); err == nil {
			binDir = expanded
		}
	} else {
		binDir = toolBinPath
	}

	slog.Debug("runtime installed via delegation", "name", name, "version", resolvedVersion)

	return &resource.RuntimeState{
		Type:           spec.Type,
		Version:        resolvedVersion,
		VersionKind:    versionKind,
		SpecVersion:    spec.Version,
		Binaries:       spec.Binaries,
		BinDir:         binDir,
		ToolBinPath:    toolBinPath,
		Commands:       spec.Commands,
		Env:            env,
		RemoveCommand:  spec.Bootstrap.Remove,
		TaintOnUpgrade: spec.TaintOnUpgrade,
		UpdatedAt:      time.Now(),
	}, nil
}

// resolveBinDir determines where to create symlinks for runtime binaries.
// Returns empty string if no symlinks should be created.
func (i *Installer) resolveBinDir(spec *resource.RuntimeSpec) (string, error) {
	// If BinDir is explicitly set, use it
	if spec.BinDir != "" {
		expanded, err := path.Expand(spec.BinDir)
		if err != nil {
			return "", fmt.Errorf("failed to expand binDir: %w", err)
		}
		return expanded, nil
	}

	// Default: use ToolBinPath (for Go, Rust, etc.)
	if spec.ToolBinPath != "" {
		expanded, err := path.Expand(spec.ToolBinPath)
		if err != nil {
			return "", fmt.Errorf("failed to expand toolBinPath: %w", err)
		}
		return expanded, nil
	}

	// No ToolBinPath either - no symlinks
	return "", nil
}

// createSymlinks creates symlinks for runtime binaries in the specified binDir.
func (i *Installer) createSymlinks(installPath string, binaries []string, binDir string) error {
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return fmt.Errorf("failed to create bin directory: %w", err)
	}

	for _, binary := range binaries {
		// Find the binary in common locations
		binaryPath := findBinary(installPath, binary)
		if binaryPath == "" {
			return fmt.Errorf("binary %q not found in %s", binary, installPath)
		}

		linkPath := filepath.Join(binDir, binary)

		// Remove existing symlink if any
		if _, err := os.Lstat(linkPath); err == nil {
			if err := os.Remove(linkPath); err != nil {
				return fmt.Errorf("failed to remove existing symlink: %w", err)
			}
		}

		// Create symlink
		if err := os.Symlink(binaryPath, linkPath); err != nil {
			return fmt.Errorf("failed to create symlink for %s: %w", binary, err)
		}

		slog.Debug("created symlink", "binary", binary, "link", linkPath, "target", binaryPath)
	}

	return nil
}

// findExtractedRoot finds the root directory of extracted content.
// If the extracted content has a single top-level directory, return that.
// Otherwise, return the extractDir itself.
func findExtractedRoot(extractDir string) (string, error) {
	entries, err := os.ReadDir(extractDir)
	if err != nil {
		return "", err
	}

	// Filter out hidden files
	var visibleEntries []os.DirEntry
	for _, e := range entries {
		if !isHidden(e.Name()) {
			visibleEntries = append(visibleEntries, e)
		}
	}

	// If there's exactly one directory, use it as root
	if len(visibleEntries) == 1 && visibleEntries[0].IsDir() {
		return filepath.Join(extractDir, visibleEntries[0].Name()), nil
	}

	return extractDir, nil
}

// findBinary searches for a binary in common locations within the install path.
func findBinary(installPath, binary string) string {
	// Common locations for binaries
	locations := []string{
		filepath.Join(installPath, "bin", binary),
		filepath.Join(installPath, binary),
	}

	for _, loc := range locations {
		if info, err := os.Stat(loc); err == nil && !info.IsDir() {
			return loc
		}
	}

	return ""
}

// isHidden returns true if the filename is hidden (starts with .)
func isHidden(name string) bool {
	return len(name) > 0 && name[0] == '.'
}

// copyDir copies a directory recursively.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		return copyFile(path, dstPath, info.Mode())
	})
}

// copyFile copies a single file.
func copyFile(src, dst string, mode os.FileMode) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = dstFile.ReadFrom(srcFile)
	return err
}
