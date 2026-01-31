package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/terassyi/toto/internal/checksum"
	"github.com/terassyi/toto/internal/installer/download"
	"github.com/terassyi/toto/internal/installer/extract"
	"github.com/terassyi/toto/internal/path"
	"github.com/terassyi/toto/internal/resource"
)

// Installer installs runtimes using the download pattern.
type Installer struct {
	downloader  download.Downloader
	runtimesDir string
	binDir      string
}

// NewInstaller creates a new runtime Installer.
func NewInstaller(downloader download.Downloader, runtimesDir, binDir string) *Installer {
	return &Installer{
		downloader:  downloader,
		runtimesDir: runtimesDir,
		binDir:      binDir,
	}
}

// Install installs a runtime according to the resource and returns its state.
// Currently only supports the download pattern.
func (i *Installer) Install(ctx context.Context, res *resource.Runtime, name string) (*resource.RuntimeState, error) {
	spec := res.RuntimeSpec

	slog.Info("installing runtime", "name", name, "version", spec.Version)

	// Only download pattern is supported for now
	if spec.InstallerRef != "download" {
		return nil, fmt.Errorf("installer %q not supported yet (only download pattern)", spec.InstallerRef)
	}

	// Validate spec
	if spec.Source.URL == "" {
		return nil, fmt.Errorf("source.url is required for download pattern")
	}

	// Calculate install path
	installPath := filepath.Join(i.runtimesDir, name, spec.Version)

	// Check if already installed
	if _, err := os.Stat(installPath); err == nil {
		slog.Info("runtime already installed, skipping", "name", name, "version", spec.Version)
		return i.buildState(spec, installPath), nil
	}

	// Download
	tmpDir, err := os.MkdirTemp("", "toto-runtime-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	urlFilename := filepath.Base(spec.Source.URL)
	archivePath := filepath.Join(tmpDir, urlFilename)
	_, err = i.downloader.Download(ctx, spec.Source.URL, archivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to download: %w", err)
	}

	// Verify checksum
	if err := i.downloader.Verify(ctx, archivePath, spec.Source.Checksum); err != nil {
		return nil, fmt.Errorf("failed to verify checksum: %w", err)
	}

	// Determine archive type
	archiveType := extract.ArchiveType(spec.Source.ArchiveType)
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
	if err := i.createSymlinks(installPath, spec.Binaries); err != nil {
		return nil, fmt.Errorf("failed to create symlinks: %w", err)
	}

	slog.Info("runtime installed successfully", "name", name, "version", spec.Version, "path", installPath)

	return i.buildState(spec, installPath), nil
}

// Remove removes an installed runtime.
func (i *Installer) Remove(ctx context.Context, st *resource.RuntimeState, name string) error {
	slog.Info("removing runtime", "name", name, "version", st.Version)

	// Remove symlinks
	for _, binary := range st.Binaries {
		linkPath := filepath.Join(i.binDir, binary)
		if err := os.Remove(linkPath); err != nil && !os.IsNotExist(err) {
			slog.Debug("failed to remove symlink", "path", linkPath, "error", err)
		}
	}

	// Remove install directory
	if st.InstallPath != "" {
		if err := os.RemoveAll(st.InstallPath); err != nil {
			return fmt.Errorf("failed to remove install directory: %w", err)
		}
		// Try to remove version directory if empty
		versionDir := filepath.Dir(st.InstallPath)
		_ = os.Remove(versionDir)
	}

	slog.Info("runtime removed", "name", name)
	return nil
}

// buildState creates a RuntimeState from the installation.
func (i *Installer) buildState(spec *resource.RuntimeSpec, installPath string) *resource.RuntimeState {
	digest := ""
	if spec.Source.Checksum != nil {
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
		InstallerRef: spec.InstallerRef,
		Version:      spec.Version,
		Digest:       digest,
		InstallPath:  installPath,
		Binaries:     spec.Binaries,
		ToolBinPath:  toolBinPath,
		Env:          env,
		UpdatedAt:    time.Now(),
	}
}

// createSymlinks creates symlinks for runtime binaries in binDir.
func (i *Installer) createSymlinks(installPath string, binaries []string) error {
	if err := os.MkdirAll(i.binDir, 0755); err != nil {
		return fmt.Errorf("failed to create bin directory: %w", err)
	}

	for _, binary := range binaries {
		// Find the binary in common locations
		binaryPath := findBinary(installPath, binary)
		if binaryPath == "" {
			return fmt.Errorf("binary %q not found in %s", binary, installPath)
		}

		linkPath := filepath.Join(i.binDir, binary)

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
