package tool

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/terassyi/tomei/internal/checksum"
	"github.com/terassyi/tomei/internal/installer"
	"github.com/terassyi/tomei/internal/installer/command"
	"github.com/terassyi/tomei/internal/installer/download"
	"github.com/terassyi/tomei/internal/installer/extract"
	"github.com/terassyi/tomei/internal/installer/place"
	"github.com/terassyi/tomei/internal/registry/aqua"
	"github.com/terassyi/tomei/internal/resource"
)

// RuntimeInfo contains the information needed to install tools via runtime delegation.
type RuntimeInfo struct {
	InstallPath string            // Path where runtime is installed (e.g., ~/.local/share/tomei/runtimes/go/1.25.5)
	BinDir      string            // Path where runtime binaries are located (e.g., ~/.local/share/pnpm)
	ToolBinPath string            // Path where tools should be installed (e.g., ~/go/bin)
	Env         map[string]string // Environment variables (e.g., GOROOT, GOBIN)
	Commands    *resource.CommandsSpec
}

// InstallerInfo contains the information needed to install tools via installer delegation.
type InstallerInfo struct {
	Type     resource.InstallType // "download" or "delegation"
	ToolRef  string               // Reference to tool (optional, e.g., cargo-binstall)
	Commands *resource.CommandsSpec
}

// Installer installs tools using download or delegation patterns.
type Installer struct {
	downloader       download.Downloader
	placer           place.Placer
	cmdExecutor      *command.Executor
	runtimes         map[string]*RuntimeInfo   // name -> RuntimeInfo
	installers       map[string]*InstallerInfo // name -> InstallerInfo
	toolBinPaths     map[string]string         // installer name -> tool bin directory
	resolver         *aqua.Resolver            // aqua-registry resolver (optional)
	registryRef      aqua.RegistryRef          // aqua-registry version ref (e.g., "v4.465.0")
	progressCallback download.ProgressCallback // optional progress callback
	outputCallback   download.OutputCallback   // optional output callback for delegation
}

// NewInstaller creates a new tool Installer.
func NewInstaller(downloader download.Downloader, placer place.Placer) *Installer {
	return &Installer{
		downloader:  downloader,
		placer:      placer,
		cmdExecutor: command.NewExecutor(""),
		runtimes:    make(map[string]*RuntimeInfo),
		installers:  make(map[string]*InstallerInfo),
	}
}

// RegisterRuntime registers a runtime for tool delegation.
func (i *Installer) RegisterRuntime(name string, info *RuntimeInfo) {
	i.runtimes[name] = info
}

// RegisterInstaller registers an installer for tool delegation.
func (i *Installer) RegisterInstaller(name string, info *InstallerInfo) {
	i.installers[name] = info
}

// SetToolBinPaths sets the mapping from installer name to tool bin directory.
// This is used to add the tool's bin directory to PATH when executing installer delegation commands.
func (i *Installer) SetToolBinPaths(paths map[string]string) {
	i.toolBinPaths = paths
}

// buildEnvWithToolPath builds an environment map with the tool's bin directory prepended to PATH.
// This ensures installer delegation commands (e.g., helm pull) can find their toolRef binary.
func (i *Installer) buildEnvWithToolPath(installerName string) map[string]string {
	if i.toolBinPaths == nil {
		return nil
	}
	binDir, ok := i.toolBinPaths[installerName]
	if !ok || binDir == "" {
		return nil
	}
	currentPath := os.Getenv("PATH")
	return map[string]string{
		"PATH": binDir + string(os.PathListSeparator) + currentPath,
	}
}

// SetResolver sets the aqua-registry resolver and registry ref.
// This enables registry-based tool installation via RegistryPackage.
func (i *Installer) SetResolver(resolver *aqua.Resolver, registryRef aqua.RegistryRef) {
	i.resolver = resolver
	i.registryRef = registryRef
}

// Resolver returns the aqua-registry resolver.
// Returns nil if resolver is not configured.
func (i *Installer) Resolver() *aqua.Resolver {
	return i.resolver
}

// SetProgressCallback sets a callback for download progress.
func (i *Installer) SetProgressCallback(callback download.ProgressCallback) {
	i.progressCallback = callback
}

// SetOutputCallback sets a callback for command output lines (delegation pattern).
func (i *Installer) SetOutputCallback(callback download.OutputCallback) {
	i.outputCallback = callback
}

// Install installs a tool according to the resource and returns its state.
func (i *Installer) Install(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
	spec := res.ToolSpec

	slog.Debug("installing tool", "name", name, "version", spec.Version)

	// Determine installation pattern
	// 1. If runtimeRef is set, use Runtime delegation (e.g., go install)
	if spec.RuntimeRef != "" {
		return i.installByRuntime(ctx, res, name)
	}

	// 2. If installerRef points to a delegation type Installer, use it
	if info, ok := i.installers[spec.InstallerRef]; ok {
		if info.Type == resource.InstallTypeDelegation {
			return i.installByInstaller(ctx, res, name, info)
		}
	}

	// 3. If package with owner/repo is set, use aqua-registry to resolve URL
	if spec.Package.IsRegistry() {
		return i.installFromRegistry(ctx, res, name)
	}

	// 4. Otherwise, use download pattern with explicit source
	return i.installByDownload(ctx, res, name)
}

// installByDownload installs a tool using the download pattern.
func (i *Installer) installByDownload(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
	spec := res.ToolSpec
	cfg := &installer.InstallConfig{
		BinaryName: name,
	}

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
		slog.Debug("tool already installed, skipping", "name", name, "version", spec.Version)
		// Even if binary exists, ensure symlink points to correct version
		if _, err := i.placer.Symlink(target); err != nil {
			return nil, fmt.Errorf("failed to update symlink: %w", err)
		}
		return i.buildState(spec, target, expectedHash), nil

	case place.ValidateActionReplace:
		if !cfg.Force {
			return nil, fmt.Errorf("tool %s@%s exists with different hash, use force to replace", name, spec.Version)
		}
		slog.Debug("replacing existing tool", "name", name, "version", spec.Version)

	case place.ValidateActionInstall:
		slog.Debug("installing new tool", "name", name, "version", spec.Version)
	}

	// Download
	tmpDir, err := os.MkdirTemp("", "tomei-download-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer func() { _ = i.placer.Cleanup(tmpDir) }()

	// Use original filename from URL for checksum matching
	urlFilename := filepath.Base(spec.Source.URL)
	archivePath := filepath.Join(tmpDir, urlFilename)

	// Download with progress callback if set (prefer context callback for parallel execution)
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

	// Determine archive type: use explicit value or auto-detect from URL
	archiveType := spec.Source.ArchiveType
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

	// For raw binaries, use tool name as subdirectory so the binary gets the correct name
	extractDir := filepath.Join(tmpDir, "extracted")
	if archiveType == extract.ArchiveTypeRaw {
		extractDir = filepath.Join(tmpDir, "extracted", name)
	}

	archiveFile, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open archive: %w", err)
	}
	defer archiveFile.Close()

	if err := extractor.Extract(archiveFile, extractDir); err != nil {
		return nil, fmt.Errorf("failed to extract: %w", err)
	}

	// Reset extractDir for placer to search from
	if archiveType == extract.ArchiveTypeRaw {
		extractDir = filepath.Join(tmpDir, "extracted")
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

	slog.Debug("tool installed successfully", "name", name, "version", spec.Version, "path", result.BinaryPath)

	return i.buildState(spec, target, expectedHash), nil
}

// installFromRegistry installs a tool using aqua-registry to resolve the download URL.
func (i *Installer) installFromRegistry(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
	spec := res.ToolSpec

	// Check if resolver is configured
	if i.resolver == nil {
		return nil, fmt.Errorf("aqua-registry resolver not configured")
	}
	if i.registryRef == "" {
		return nil, fmt.Errorf("aqua-registry ref not configured; run 'tomei init' first")
	}

	// Determine version: use spec.Version or fetch latest
	pkgName := spec.Package.String()
	version := spec.Version
	if version == "" {
		slog.Debug("fetching latest version from registry", "package", pkgName)
		// Fetch package info to get repo owner/name for version lookup
		info, err := i.resolver.FetchPackageInfo(ctx, i.registryRef, pkgName)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch package info: %w", err)
		}
		latestVersion, err := i.resolver.VersionClient().GetLatestToolVersion(ctx, info.RepoOwner, info.RepoName)
		if err != nil {
			return nil, fmt.Errorf("failed to get latest version for %s: %w", pkgName, err)
		}
		version = latestVersion
		slog.Debug("using latest version", "package", pkgName, "version", version)
	}

	// Resolve download URL from registry
	resolved, err := i.resolver.Resolve(ctx, i.registryRef, pkgName, version)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve package %s: %w", pkgName, err)
	}

	slog.Debug("resolved package URL", "package", pkgName, "url", resolved.URL, "checksum", resolved.ChecksumURL)

	// Log warnings from resolution
	for _, w := range resolved.Warnings {
		slog.Warn("registry warning", "package", pkgName, "warning", w)
	}

	// Check for errors from resolution (e.g., unsupported OS/Arch)
	if len(resolved.Errors) > 0 {
		for _, e := range resolved.Errors {
			slog.Error("registry error", "package", pkgName, "error", e)
		}
		return nil, fmt.Errorf("package %s is not supported on this platform: %s", pkgName, resolved.Errors[0])
	}

	// Build DownloadSource from resolved info
	source := &resource.DownloadSource{
		URL:         resolved.URL,
		ArchiveType: extract.ArchiveType(resolved.Format),
	}

	// Add checksum if available
	if resolved.ChecksumURL != "" {
		source.Checksum = &resource.Checksum{
			URL: resolved.ChecksumURL,
		}
	}

	// Create a modified tool with resolved source for download
	resolvedTool := &resource.Tool{
		BaseResource: res.BaseResource,
		ToolSpec: &resource.ToolSpec{
			InstallerRef: spec.InstallerRef,
			Version:      version,
			Enabled:      spec.Enabled,
			Source:       source,
			Package:      spec.Package,
		},
	}

	// Use existing download logic
	state, err := i.installByDownload(ctx, resolvedTool, name)
	if err != nil {
		return nil, err
	}

	// Update state to include package info and original spec version
	state.Package = spec.Package
	state.VersionKind = resource.ClassifyVersion(spec.Version)
	state.SpecVersion = spec.Version // preserve original spec version (e.g., "" for latest)

	return state, nil
}

// buildState creates a ToolState from the installation result.
func (i *Installer) buildState(spec *resource.ToolSpec, target place.Target, digest string) *resource.ToolState {
	return &resource.ToolState{
		InstallerRef: spec.InstallerRef,
		Version:      spec.Version,
		VersionKind:  resource.ClassifyVersion(spec.Version),
		SpecVersion:  spec.Version,
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
	slog.Debug("removing tool", "name", name, "version", st.Version)

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

	slog.Debug("tool removed", "name", name)
	return nil
}

// installByRuntime installs a tool using Runtime delegation (e.g., go install).
func (i *Installer) installByRuntime(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
	spec := res.ToolSpec

	// Get runtime info
	info, ok := i.runtimes[spec.RuntimeRef]
	if !ok {
		return nil, fmt.Errorf("runtime %q not found", spec.RuntimeRef)
	}

	// Check if runtime has commands defined
	if info.Commands == nil || len(info.Commands.Install) == 0 {
		return nil, fmt.Errorf("runtime %q does not have install command defined", spec.RuntimeRef)
	}

	// Ensure toolBinPath directory exists
	if info.ToolBinPath != "" {
		if err := os.MkdirAll(info.ToolBinPath, 0755); err != nil {
			return nil, fmt.Errorf("failed to create toolBinPath directory %q: %w", info.ToolBinPath, err)
		}
	}

	// Build variables for command substitution
	vars := command.Vars{
		Package: spec.Package.String(),
		Version: spec.Version,
		Name:    name,
		BinPath: filepath.Join(info.ToolBinPath, name),
		Args:    strings.Join(spec.Args, " "),
	}

	// Build environment with PATH including runtime's bin directory
	env := make(map[string]string)
	maps.Copy(env, info.Env)
	// Add runtime's bin directory to PATH so commands like "go" or "pnpm" can be found.
	// Download pattern: use InstallPath/bin (e.g., /runtimes/go/1.25.5/bin)
	// Delegation pattern: use BinDir (e.g., ~/.local/share/pnpm)
	var runtimeBinDir string
	if info.InstallPath != "" {
		runtimeBinDir = filepath.Join(info.InstallPath, "bin")
	} else {
		runtimeBinDir = info.BinDir
	}
	if runtimeBinDir != "" {
		if currentPath := os.Getenv("PATH"); currentPath != "" {
			env["PATH"] = runtimeBinDir + string(os.PathListSeparator) + currentPath
		} else {
			env["PATH"] = runtimeBinDir
		}
	}

	// Execute install command with runtime's environment and output streaming
	// Prefer context callback for parallel execution
	outputCb := download.CallbackFromContext[download.OutputCallback](ctx)
	if outputCb == nil {
		outputCb = i.outputCallback
	}
	if outputCb != nil {
		if err := i.cmdExecutor.ExecuteWithOutput(ctx, info.Commands.Install, vars, env, command.OutputCallback(outputCb)); err != nil {
			return nil, fmt.Errorf("failed to execute install command: %w", err)
		}
	} else {
		if err := i.cmdExecutor.ExecuteWithEnv(ctx, info.Commands.Install, vars, env); err != nil {
			return nil, fmt.Errorf("failed to execute install command: %w", err)
		}
	}

	slog.Debug("tool installed via runtime", "name", name, "version", spec.Version, "runtime", spec.RuntimeRef)

	return i.buildDelegationState(spec, vars.BinPath), nil
}

// installByInstaller installs a tool using Installer delegation (e.g., brew install).
func (i *Installer) installByInstaller(ctx context.Context, res *resource.Tool, name string, info *InstallerInfo) (*resource.ToolState, error) {
	spec := res.ToolSpec

	// Check if installer has commands defined
	if info.Commands == nil || len(info.Commands.Install) == 0 {
		return nil, fmt.Errorf("installer %q does not have install command defined", spec.InstallerRef)
	}

	// Build variables for command substitution
	pkg := spec.Package.String()
	if pkg == "" {
		pkg = name // default to tool name if package not specified
	}

	vars := command.Vars{
		Package: pkg,
		Version: spec.Version,
		Name:    name,
		BinPath: "", // installer manages the path
		Args:    strings.Join(spec.Args, " "),
	}

	// Build environment with PATH including the installer's toolRef binary directory
	env := i.buildEnvWithToolPath(spec.InstallerRef)

	// Execute install command with output streaming
	// Prefer context callback for parallel execution
	outputCb := download.CallbackFromContext[download.OutputCallback](ctx)
	if outputCb == nil {
		outputCb = i.outputCallback
	}
	if outputCb != nil {
		if err := i.cmdExecutor.ExecuteWithOutput(ctx, info.Commands.Install, vars, env, command.OutputCallback(outputCb)); err != nil {
			return nil, fmt.Errorf("failed to execute install command: %w", err)
		}
	} else {
		if err := i.cmdExecutor.ExecuteWithEnv(ctx, info.Commands.Install, vars, env); err != nil {
			return nil, fmt.Errorf("failed to execute install command: %w", err)
		}
	}

	slog.Debug("tool installed via installer", "name", name, "version", spec.Version, "installer", spec.InstallerRef)

	return i.buildDelegationState(spec, ""), nil
}

// buildDelegationState creates a ToolState for delegation pattern installations.
func (i *Installer) buildDelegationState(spec *resource.ToolSpec, binPath string) *resource.ToolState {
	return &resource.ToolState{
		InstallerRef: spec.InstallerRef,
		Version:      spec.Version,
		VersionKind:  resource.ClassifyVersion(spec.Version),
		SpecVersion:  spec.Version,
		BinPath:      binPath,
		RuntimeRef:   spec.RuntimeRef,
		Package:      spec.Package,
		UpdatedAt:    time.Now(),
	}
}
