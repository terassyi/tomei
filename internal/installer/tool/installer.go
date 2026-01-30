package tool

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/terassyi/toto/internal/checksum"
	"github.com/terassyi/toto/internal/installer"
	"github.com/terassyi/toto/internal/installer/download"
	"github.com/terassyi/toto/internal/installer/extract"
	"github.com/terassyi/toto/internal/installer/place"
	"github.com/terassyi/toto/internal/resource"
)

// Installer installs tools using the download pattern.
type Installer struct {
	downloader download.Downloader
	placer     place.Placer
}

// NewInstaller creates a new tool Installer.
func NewInstaller(downloader download.Downloader, placer place.Placer) *Installer {
	return &Installer{
		downloader: downloader,
		placer:     placer,
	}
}

// Install installs a tool according to the resource and returns its state.
func (i *Installer) Install(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
	spec := res.ToolSpec
	cfg := &installer.InstallConfig{
		BinaryName: name, // default to tool name
	}

	slog.Info("installing tool", "name", name, "version", spec.Version)

	// Validate spec
	if spec.Source == nil {
		return nil, fmt.Errorf("source is required for download pattern")
	}

	// Get expected hash for validation
	expectedHash := checksum.ExtractHash(spec.Source.Checksum)

	// Create place target
	target := place.Target{
		Name:       name,
		Version:    spec.Version,
		BinaryName: cfg.BinaryName,
	}

	// Validate existing installation
	action, err := i.placer.Validate(target, expectedHash)
	if err != nil {
		return nil, fmt.Errorf("failed to validate: %w", err)
	}

	switch action {
	case place.ValidateActionSkip:
		slog.Info("tool already installed, skipping", "name", name, "version", spec.Version)
		return i.buildState(spec, target, expectedHash), nil

	case place.ValidateActionReplace:
		if !cfg.Force {
			return nil, fmt.Errorf("tool %s@%s exists with different hash, use force to replace", name, spec.Version)
		}
		slog.Info("replacing existing tool", "name", name, "version", spec.Version)

	case place.ValidateActionInstall:
		slog.Debug("installing new tool", "name", name, "version", spec.Version)
	}

	// Download
	tmpDir, err := os.MkdirTemp("", "toto-download-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer func() { _ = i.placer.Cleanup(tmpDir) }()

	// Use original filename from URL for checksum matching
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

	// Determine archive type: use explicit value or auto-detect from URL
	archiveType := extract.ArchiveType(spec.Source.ArchiveType)
	if archiveType == "" {
		archiveType = extract.DetectArchiveType(spec.Source.URL)
		if archiveType == "" {
			return nil, fmt.Errorf("cannot determine archive type from URL: %s", spec.Source.URL)
		}
		slog.Debug("auto-detected archive type", "type", archiveType, "url", spec.Source.URL)
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

	// Place binary
	result, err := i.placer.Place(extractDir, target)
	if err != nil {
		return nil, fmt.Errorf("failed to place binary: %w", err)
	}

	// Create symlink
	linkPath, err := i.placer.Symlink(target)
	if err != nil {
		return nil, fmt.Errorf("failed to create symlink: %w", err)
	}
	result.LinkPath = linkPath

	slog.Info("tool installed successfully", "name", name, "version", spec.Version, "path", result.BinaryPath)

	return i.buildState(spec, target, expectedHash), nil
}

// buildState creates a ToolState from the installation result.
func (i *Installer) buildState(spec *resource.ToolSpec, target place.Target, digest string) *resource.ToolState {
	return &resource.ToolState{
		InstallerRef: spec.InstallerRef,
		Version:      spec.Version,
		Digest:       digest,
		InstallPath:  i.placer.BinaryPath(target),
		BinPath:      i.placer.LinkPath(target),
		Source:       spec.Source,
		RuntimeRef:   spec.RuntimeRef,
		Package:      spec.Package,
		UpdatedAt:    time.Now(),
	}
}

// Remove removes an installed tool.
func (i *Installer) Remove(ctx context.Context, st *resource.ToolState, name string) error {
	slog.Info("removing tool", "name", name, "version", st.Version)

	// Remove the binary
	if st.InstallPath != "" {
		if err := i.placer.Cleanup(st.InstallPath); err != nil {
			return fmt.Errorf("failed to remove binary: %w", err)
		}
		// Also remove the version directory if empty
		versionDir := filepath.Dir(st.InstallPath)
		if err := i.placer.Cleanup(versionDir); err != nil {
			slog.Debug("failed to remove version directory", "path", versionDir, "error", err)
		}
	}

	// Remove the symlink
	if st.BinPath != "" {
		if err := i.placer.Cleanup(st.BinPath); err != nil {
			slog.Debug("failed to remove symlink", "path", st.BinPath, "error", err)
		}
	}

	slog.Info("tool removed", "name", name)
	return nil
}
