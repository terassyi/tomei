package tool

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/terassyi/tomei/internal/checksum"
	"github.com/terassyi/tomei/internal/installer"
	"github.com/terassyi/tomei/internal/installer/command"
	"github.com/terassyi/tomei/internal/installer/download"
	"github.com/terassyi/tomei/internal/installer/executor"
	"github.com/terassyi/tomei/internal/installer/extract"
	"github.com/terassyi/tomei/internal/installer/place"
	"github.com/terassyi/tomei/internal/installer/resolve"
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
	// MinimumReleaseAge is the Go duration string from the Runtime spec, exposed
	// to delegation install commands as {{.MinimumReleaseAge}}. Empty when unset.
	MinimumReleaseAge string
}

// InstallerInfo contains the information needed to install tools via installer delegation.
type InstallerInfo struct {
	Type    resource.InstallType // "download" or "delegation"
	ToolRef string               // Reference to tool (optional, e.g., cargo-binstall)
	// BinDir is the installer's own bin directory (expanded), e.g. /opt/homebrew/bin
	// for #BrewInstaller. Prepended to PATH when running the installer's delegation
	// commands so a bare command (e.g. `brew install`) resolves even when Homebrew
	// was bootstrapped in the same run (#269). Empty when the spec omits binDir.
	BinDir   string
	Commands *resource.CommandsSpec
	// MinimumReleaseAge is the Go duration string from the Installer spec, exposed
	// to delegation install commands as {{.MinimumReleaseAge}}. Empty when unset.
	MinimumReleaseAge string
}

// CommandRunner is the interface for executing shell commands.
// Enables testing with mocks instead of real command execution.
type CommandRunner interface {
	Execute(ctx context.Context, cmds []string, vars command.Vars) error
	ExecuteWithEnv(ctx context.Context, cmds []string, vars command.Vars, env map[string]string) error
	ExecuteWithOutput(ctx context.Context, cmds []string, vars command.Vars, env map[string]string, callback command.OutputCallback) error
	Check(ctx context.Context, cmds []string, vars command.Vars, env map[string]string) bool
}

// Installer installs tools using download, delegation, or commands patterns.
type Installer struct {
	downloader       download.Downloader
	placer           place.Placer
	userBinDir       string // bin directory for non-privileged tool symlinks (e.g., ~/.local/bin)
	systemBinDir     string // bin directory for privileged tool symlinks (e.g., /usr/local/bin); SUB5 #228
	cmdExecutor      CommandRunner
	versionResolver  *resolve.Resolver         // shared version resolver (optional)
	runtimes         map[string]*RuntimeInfo   // name -> RuntimeInfo
	installers       map[string]*InstallerInfo // name -> InstallerInfo
	toolBinPaths     map[string]string         // installer name -> tool bin directory
	resolver         *aqua.Resolver            // aqua-registry resolver (optional)
	registryRef      aqua.RegistryRef          // aqua-registry version ref (e.g., "v4.465.0")
	progressCallback download.ProgressCallback // optional progress callback
	outputCallback   download.OutputCallback   // optional output callback for delegation
}

// NewInstaller creates a new tool Installer.
func NewInstaller(downloader download.Downloader, placer place.Placer, userBinDir, systemBinDir string) *Installer {
	cmdExec := command.NewExecutor("")
	return &Installer{
		downloader:      downloader,
		placer:          placer,
		userBinDir:      userBinDir,
		systemBinDir:    systemBinDir,
		cmdExecutor:     cmdExec,
		versionResolver: resolve.NewResolver(cmdExec, http.DefaultClient),
		runtimes:        make(map[string]*RuntimeInfo),
		installers:      make(map[string]*InstallerInfo),
	}
}

// NewInstallerWithRunner creates a new tool Installer with a custom CommandRunner (for testing).
func NewInstallerWithRunner(downloader download.Downloader, placer place.Placer, userBinDir, systemBinDir string, runner CommandRunner) *Installer {
	return &Installer{
		downloader:   downloader,
		placer:       placer,
		userBinDir:   userBinDir,
		systemBinDir: systemBinDir,
		cmdExecutor:  runner,
		runtimes:     make(map[string]*RuntimeInfo),
		installers:   make(map[string]*InstallerInfo),
	}
}

// SetVersionResolver sets the shared version resolver.
func (i *Installer) SetVersionResolver(r *resolve.Resolver) {
	i.versionResolver = r
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

// buildEnvWithToolPath builds an environment map whose PATH lets an installer's
// delegation commands find their binary. PATH is composed, highest precedence
// first, of: the installer's own binDir (installerBinDir; e.g. /opt/homebrew/bin
// for #BrewInstaller — the production location of the delegated CLI, #269), then
// the installer's toolRef tool bin dir (toolBinPaths[installerName]; e.g. for
// cargo-binstall installed via cargo), then the inherited $PATH. The installer's
// own binDir wins because it is the authoritative location for its CLI; the
// toolRef dir is a fallback. Empty components are skipped; returns nil (PATH left
// inherited) when neither is available.
func (i *Installer) buildEnvWithToolPath(installerName, installerBinDir string) map[string]string {
	var parts []string
	if installerBinDir != "" {
		parts = append(parts, installerBinDir)
	}
	if i.toolBinPaths != nil {
		if binDir := i.toolBinPaths[installerName]; binDir != "" {
			parts = append(parts, binDir)
		}
	}
	if len(parts) == 0 {
		return nil
	}
	// Only append the inherited PATH when non-empty: a trailing separator
	// (e.g. "/opt/homebrew/bin:") makes shells treat the cwd as on PATH.
	if current := os.Getenv("PATH"); current != "" {
		parts = append(parts, current)
	}
	return map[string]string{
		"PATH": strings.Join(parts, string(os.PathListSeparator)),
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

// resolveOutputCallback returns the effective output callback from context or field fallback.
func (i *Installer) resolveOutputCallback(ctx context.Context) download.OutputCallback {
	if cb := download.CallbackFromContext[download.OutputCallback](ctx); cb != nil {
		return cb
	}
	return i.outputCallback
}

// executeCommand runs cmds with output streaming if a callback is available,
// otherwise falls back to plain execution.
func (i *Installer) executeCommand(ctx context.Context, cmds []string, vars command.Vars, env map[string]string) error {
	if cb := i.resolveOutputCallback(ctx); cb != nil {
		return i.cmdExecutor.ExecuteWithOutput(ctx, cmds, vars, env, command.OutputCallback(cb))
	}
	return i.cmdExecutor.ExecuteWithEnv(ctx, cmds, vars, env)
}

// binaryNameRegexp matches the CUE schema constraint: ^[a-zA-Z0-9][a-zA-Z0-9._-]*$
var binaryNameRegexp = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// validateName checks that a path-bound name (kind labels which one, e.g.
// "binaryName" or "srcBinaryName") is safe for use in file paths. Enforces the
// same regex as the CUE schema (^[a-zA-Z0-9][a-zA-Z0-9._-]*$) as defense-in-depth
// for inputs that bypass CUE validation (e.g., JSON/YAML or untrusted registry data).
func validateName(kind, name string) error {
	if !binaryNameRegexp.MatchString(name) {
		return fmt.Errorf("invalid %s %q: must match ^[a-zA-Z0-9][a-zA-Z0-9._-]*$", kind, name)
	}
	return nil
}

// Install installs a tool according to the resource and returns its state.
func (i *Installer) Install(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
	spec := res.ToolSpec

	// Validate binaryName to prevent path traversal (defense-in-depth; CUE schema also enforces this)
	if spec.BinaryName != "" {
		if err := validateName("binaryName", spec.BinaryName); err != nil {
			return nil, err
		}
	}

	slog.Debug("installing tool", "name", name, "version", spec.Version)

	// Determine installation pattern
	// 1. If commands is set, use self-managed commands pattern
	if spec.Commands != nil {
		return i.installByCommands(ctx, res, name)
	}

	// 2. If runtimeRef is set, use Runtime delegation (e.g., go install)
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
	return i.installByDownload(ctx, res, name, nil)
}

// installByDownload installs a tool using the download pattern.
// cfg overrides default configuration (binary name mapping, etc.); nil uses defaults.
func (i *Installer) installByDownload(ctx context.Context, res *resource.Tool, name string, cfg *installer.InstallConfig) (*resource.ToolState, error) {
	spec := res.ToolSpec
	if cfg == nil {
		cfg = &installer.InstallConfig{}
	}
	if cfg.BinaryName == "" {
		cfg.BinaryName = name
	}
	// User spec binaryName takes highest priority; preserve the pre-override
	// binary name as SrcBinaryName for archive search.
	if spec.BinaryName != "" && cfg.SrcBinaryName == "" {
		cfg.SrcBinaryName = cfg.BinaryName
	}
	cfg.BinaryName = effectiveBinaryName(spec, cfg.BinaryName)

	// Validate effective binary name (after registry mapping and spec override)
	if err := validateName("binaryName", cfg.BinaryName); err != nil {
		return nil, err
	}
	// SrcBinaryName (from untrusted registry files[].src) is now used as the
	// single-file extraction subdirectory name via Target.SearchName(), so it
	// must also be path-safe. path.Base does not neutralize ".." (#281).
	if cfg.SrcBinaryName != "" {
		if err := validateName("srcBinaryName", cfg.SrcBinaryName); err != nil {
			return nil, err
		}
	}

	// Validate spec
	if spec.Source == nil {
		return nil, fmt.Errorf("source is required for download pattern")
	}

	// Get expected hash for validation
	var expectedHash checksum.Digest
	if spec.Source.Checksum != nil {
		expectedHash = checksum.ExtractHash(spec.Source.Checksum.Value)
	}

	// Create place target
	target := place.Target{
		Name:          name,
		Version:       spec.Version,
		BinaryName:    cfg.BinaryName,
		SrcBinaryName: cfg.SrcBinaryName,
	}

	// Validate existing installation
	action, err := i.placer.Validate(target, string(expectedHash))
	if err != nil {
		return nil, fmt.Errorf("failed to validate: %w", err)
	}

	switch action {
	case place.ValidateActionSkip:
		slog.Debug("tool already installed, skipping", "name", name, "version", spec.Version)
		// Heal binaries placed non-executable by an older tomei (#273): the skip path
		// never calls Place, so a 0644 binary would stay non-runnable forever. Best-effort
		// — the binary already validated, so a chmod failure (e.g. read-only tools dir)
		// must not turn this previously-infallible branch into a hard failure.
		if err := place.EnsureExecutable(i.placer.BinaryPath(target)); err != nil {
			slog.Warn("failed to ensure tool binary is executable", "name", name, "error", err)
		}
		// Even if binary exists, ensure symlink points to correct version.
		// SUB5 #228: privileged tools route through the system bin dir.
		linkPath, binDirKind, err := i.createSymlink(ctx, res, target)
		if err != nil {
			return nil, fmt.Errorf("failed to update symlink: %w", err)
		}
		// Clean up old symlink if binaryName changed (e.g., upgrade with same binary but new name)
		i.cleanupOldSymlink(ctx, linkPath)
		return i.buildState(spec, target, expectedHash, binDirKind, linkPath), nil

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

	// For single-file binaries (raw, bare gz), name the subdirectory after the
	// placer's search name. The gz/raw extractor names the extracted file after
	// the subdirectory's base name, so this makes the extracted file name match
	// what place.Place searches for — including aqua's files[].src case where
	// SrcBinaryName differs from the tool name (e.g. tree-sitter-linux-arm64). See #281.
	extractDir := filepath.Join(tmpDir, "extracted")
	if extract.IsSingleFileArchive(archiveType) {
		extractDir = filepath.Join(tmpDir, "extracted", target.SearchName())
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
	if extract.IsSingleFileArchive(archiveType) {
		extractDir = filepath.Join(tmpDir, "extracted")
	}

	// Place binary
	result, err := i.placer.Place(extractDir, target)
	if err != nil {
		return nil, fmt.Errorf("failed to place binary: %w", err)
	}

	// Create symlink. SUB5 #228: privileged tools route through the system bin dir.
	linkPath, binDirKind, err := i.createSymlink(ctx, res, target)
	if err != nil {
		return nil, fmt.Errorf("failed to create symlink: %w", err)
	}
	result.LinkPath = linkPath

	// Clean up old symlink if binaryName changed
	i.cleanupOldSymlink(ctx, linkPath)

	slog.Debug("tool installed successfully", "name", name, "version", spec.Version, "path", result.BinaryPath)

	return i.buildState(spec, target, expectedHash, binDirKind, linkPath), nil
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
	if resource.IsLatestVersion(version) {
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

	// Check for errors from resolution (e.g., unsupported OS/Arch, empty URL)
	if len(resolved.Errors) > 0 {
		for _, e := range resolved.Errors {
			slog.Error("registry error", "package", pkgName, "error", e)
		}
		return nil, fmt.Errorf("cannot install package %s: %s", pkgName, resolved.Errors[0])
	}

	// Build DownloadSource from resolved info
	source := &resource.DownloadSource{
		URL:         resolved.URL,
		ArchiveType: resolved.Format,
	}

	// Add checksum if available
	if resolved.ChecksumURL != "" {
		source.Checksum = &resource.Checksum{
			URL:       resolved.ChecksumURL,
			Algorithm: resolved.ChecksumAlgorithm,
		}
	}

	// Create a modified tool with resolved source for download.
	resolvedTool := buildResolvedTool(res, source, version)

	// Build install config from resolved files.
	cfg, err := extractBinaryMapping(effectiveBinaryName(spec, name), resolved.Files)
	if err != nil {
		return nil, fmt.Errorf("package %s: %w", pkgName, err)
	}

	// Use existing download logic (name = resource name for storage path)
	state, err := i.installByDownload(ctx, resolvedTool, name, cfg)
	if err != nil {
		return nil, err
	}

	// Update state to include package info and original spec version
	state.Package = spec.Package
	state.VersionKind = resource.ClassifyVersion(spec.Version)
	state.SpecVersion = spec.Version // preserve original spec version (e.g., "" for latest)

	return state, nil
}

// buildResolvedTool produces the download-shaped Tool that installFromRegistry
// hands to installByDownload. SUB5 #228: Privileged MUST be preserved on the
// resolved spec — createSymlink reads Tool.IsPrivileged() to route the symlink
// to SystemBinDir, and buildState persists state.Privileged from spec. Dropping
// the field would route privileged registry installs to the user bin dir (the
// exact bug SUB5 fixes) and break the state-driven removal-skip gate.
func buildResolvedTool(orig *resource.Tool, source *resource.DownloadSource, version string) *resource.Tool {
	spec := orig.ToolSpec
	return &resource.Tool{
		BaseResource: orig.BaseResource,
		ToolSpec: &resource.ToolSpec{
			InstallerRef: spec.InstallerRef,
			Version:      version,
			Enabled:      spec.Enabled,
			Source:       source,
			Package:      spec.Package,
			BinaryName:   spec.BinaryName,
			Privileged:   spec.Privileged,
		},
	}
}

// extractBinaryMapping builds an InstallConfig from aqua registry files
// metadata: binary name from files[].name, source binary name from path.Base
// of files[].src. When a package ships multiple binaries (e.g.
// gravitational/teleport), the entry matching desiredName is selected, and no
// match is an error — installing files[0] instead would silently place the
// wrong binary. A single unmatched entry is kept: that is the legitimate
// name-mapping case (tool "cli" → files: [{name: gh}]).
func extractBinaryMapping(desiredName string, files []aqua.FileSpec) (*installer.InstallConfig, error) {
	cfg := &installer.InstallConfig{
		BinaryName: desiredName,
	}
	if len(files) == 0 {
		return cfg, nil
	}
	f := files[0]
	if len(files) > 1 {
		idx := slices.IndexFunc(files, func(c aqua.FileSpec) bool { return c.Name == desiredName })
		if idx < 0 {
			names := make([]string, len(files))
			for i, c := range files {
				names[i] = c.Name
			}
			return nil, fmt.Errorf("package ships multiple binaries (%s) and none matches %q; set binaryName to select one",
				strings.Join(names, ", "), desiredName)
		}
		f = files[idx]
	}
	if f.Name != "" {
		cfg.BinaryName = f.Name
	}
	if f.Src != "" {
		cfg.SrcBinaryName = path.Base(f.Src)
	}
	return cfg, nil
}

// effectiveBinaryName returns the binary name a tool installs as: the
// spec-level binaryName override when set, otherwise the resource name.
func effectiveBinaryName(spec *resource.ToolSpec, name string) string {
	if spec.BinaryName != "" {
		return spec.BinaryName
	}
	return name
}

// createSymlink resolves the bin directory based on Tool.IsPrivileged() and
// creates the symlink via the appropriate path: Placer.Symlink for the user
// arm (~/.local/bin), or place.InstallSymlink (sudo-capable) for the system
// arm (/usr/local/bin). Returns the link path and the BinDirKind to persist.
//
// SUB5 #228 — keeping the branch in one place avoids drift across the
// cached-skip and fresh-install call sites.
func (i *Installer) createSymlink(ctx context.Context, tool *resource.Tool, target place.Target) (string, resource.BinDirKind, error) {
	binDir, binDirKind := i.userBinDir, resource.BinDirKindUser
	if tool.IsPrivileged() {
		binDir, binDirKind = i.systemBinDir, resource.BinDirKindSystem
	}
	linkPath := i.placer.LinkPath(target, binDir)
	if binDirKind == resource.BinDirKindSystem {
		if err := place.InstallSymlink(ctx, i.placer.BinaryPath(target), linkPath); err != nil {
			return "", "", fmt.Errorf("create system symlink: %w", err)
		}
		return linkPath, binDirKind, nil
	}
	if _, err := i.placer.Symlink(target, binDir); err != nil {
		return "", "", fmt.Errorf("create user symlink: %w", err)
	}
	return linkPath, binDirKind, nil
}

// cleanupOldSymlink removes the old symlink and its target binary if the
// binary name has changed OR if the BinDirKind transitioned across applies.
// It compares the old BinPath from context with the new link path; the
// cleanup helper is picked from the OLD BinDirKind (also from context).
//
// SUB5 #228: a BinDirKind transition (user↔system) is the expected case —
// the old and new symlinks live in different directories, and we want the
// old one cleaned up via the helper that matches its prior location.
func (i *Installer) cleanupOldSymlink(ctx context.Context, newLinkPath string) {
	oldBinPath := executor.OldBinPathFromContext(ctx)
	if oldBinPath == "" || oldBinPath == newLinkPath {
		return
	}

	// SUB5 #228: the cleanup helper is chosen by the OLD BinDirKind read
	// from context. The defensive guard validates the old symlink is in
	// the dir its kind claims — independent of the new symlink's location,
	// which may legitimately be in a different bin dir on a transition.
	//
	// filepath.Clean both sides: filepath.Dir already normalizes its result,
	// but the Installer's userBinDir/systemBinDir fields come from config and
	// may carry a trailing slash or other non-canonical form. Without
	// normalization the comparison fails on semantically-equal dirs and
	// cleanup is skipped, leaving stale symlinks across transitions.
	oldKind := executor.OldBinDirKindFromContext(ctx)
	expectedOldDir := filepath.Clean(i.userBinDir)
	if oldKind == resource.BinDirKindSystem {
		expectedOldDir = filepath.Clean(i.systemBinDir)
	}
	if filepath.Dir(oldBinPath) != expectedOldDir {
		slog.Warn("skipping old symlink cleanup: old bin path is outside expected directory for its BinDirKind",
			"old", oldBinPath, "oldKind", oldKind, "expected_dir", expectedOldDir)
		return
	}

	slog.Debug("cleaning up old symlink", "old", oldBinPath, "new", newLinkPath, "oldKind", oldKind)

	// Resolve symlink target before removing the symlink itself,
	// so we can also clean up the old binary file
	oldTarget, err := os.Readlink(oldBinPath)
	if err == nil && oldTarget != "" {
		// Safety check: only remove if the target is under the tools directory
		toolsDir := i.placer.ToolsDir()
		cleanTarget := filepath.Clean(oldTarget)
		if isUnderDir(cleanTarget, toolsDir) {
			slog.Debug("cleaning up old binary target", "path", cleanTarget)
			_ = os.Remove(cleanTarget) // best-effort
		} else {
			slog.Warn("skipping old binary cleanup: target is outside tools directory",
				"target", cleanTarget, "tools_dir", toolsDir)
		}
	}

	// SUB5: pick the remove helper based on the OLD BinDirKind.
	if oldKind == resource.BinDirKindSystem {
		_ = place.RemoveSymlink(ctx, oldBinPath) // best-effort
	} else {
		_ = i.placer.Cleanup(oldBinPath) // best-effort
	}
}

// isUnderDir checks whether path is strictly under dir (not equal to dir).
func isUnderDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	// rel == "." means path equals dir — reject (must be strictly under).
	// rel == ".." or starts with "../" means path is outside dir.
	// A relative like "..abc" is valid (not traversal), so check for exact ".." or "../" prefix.
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// buildState creates a ToolState from the installation result.
// buildState assembles a ToolState for the download/registry install path.
// binPath and binDirKind are passed in (rather than re-derived from spec)
// so the caller's createSymlink result is the single source of truth.
// Privileged is propagated from spec on both arms (mirroring installByCommands);
// ToolComparator (reconciler/tool.go:43) only compares Privileged when
// Commands is involved, so persisting it here cannot trigger spurious
// reinstalls for download/registry tools.
func (i *Installer) buildState(spec *resource.ToolSpec, target place.Target, digest checksum.Digest, binDirKind resource.BinDirKind, binPath string) *resource.ToolState {
	return &resource.ToolState{
		InstallerRef: spec.InstallerRef,
		Version:      spec.Version,
		VersionKind:  resource.ClassifyVersion(spec.Version),
		SpecVersion:  spec.Version,
		Digest:       digest,
		InstallPath:  i.placer.BinaryPath(target),
		BinPath:      binPath,
		BinDirKind:   binDirKind,
		Privileged:   spec.Privileged,
		Source:       spec.Source,
		RuntimeRef:   spec.RuntimeRef,
		Package:      spec.Package,
		BinaryName:   spec.BinaryName,
		UpdatedAt:    time.Now(),
	}
}

// installByCommands installs a tool using self-managed commands.
func (i *Installer) installByCommands(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
	spec := res.ToolSpec
	cmds := spec.Commands

	// Determine command to run based on action type
	actionType := executor.ActionFromContext(ctx)
	var cmdToRun []string
	switch {
	case (actionType == resource.ActionUpgrade || actionType == resource.ActionReinstall) && len(cmds.Update) > 0:
		cmdToRun = cmds.Update
	default:
		cmdToRun = cmds.Install
	}

	vars := command.Vars{Name: name, Version: spec.Version}

	// env is nil: self-managed tools define their own environment inline.
	// The command runs as the invoking user; see ToolSpec.Privileged for
	// how --system interacts with sudo calls inside the command.
	if err := i.executeCommand(ctx, cmdToRun, vars, nil); err != nil {
		return nil, fmt.Errorf("failed to execute command: %w", err)
	}

	// Verify installation with check command
	if len(cmds.Check) > 0 {
		if !i.cmdExecutor.Check(ctx, cmds.Check, vars, nil) {
			return nil, fmt.Errorf("check command failed after install")
		}
	}

	// Resolve version after install/update (if configured and not exact, always unprivileged)
	resolvedVersion := spec.Version
	versionKind := resource.ClassifyVersion(spec.Version)
	if i.versionResolver != nil && len(cmds.ResolveVersion) > 0 && !resource.IsExactVersion(spec.Version) {
		resolved, err := i.versionResolver.Resolve(ctx, cmds.ResolveVersion, vars)
		if err != nil {
			slog.Warn("resolveVersion failed, using spec version", "name", name, "error", err)
		} else if resolved != "" {
			resolvedVersion = resolved
			if resource.IsLatestVersion(spec.Version) {
				versionKind = resource.VersionLatest
			} else {
				versionKind = resource.VersionAlias
			}
		}
	}

	return &resource.ToolState{
		Version:     resolvedVersion,
		VersionKind: versionKind,
		SpecVersion: spec.Version,
		Commands:    spec.Commands,
		BinaryName:  spec.BinaryName,
		Privileged:  spec.Privileged,
		UpdatedAt:   time.Now(),
	}, nil
}

// Remove removes an installed tool.
func (i *Installer) Remove(ctx context.Context, st *resource.ToolState, name string) error {
	slog.Debug("removing tool", "name", name, "version", st.Version)

	// Self-managed tool removal. The command runs as the invoking user;
	// see ToolSpec.Privileged for how --system interacts with sudo calls
	// inside the command (including removal of root-owned artifacts).
	if st.Commands != nil {
		if len(st.Commands.Remove) > 0 {
			vars := command.Vars{Name: name, Version: st.Version}
			if err := i.cmdExecutor.Execute(ctx, st.Commands.Remove, vars); err != nil {
				return fmt.Errorf("failed to execute remove command: %w", err)
			}
		} else {
			slog.Warn("no remove command for self-managed tool, skipping", "name", name)
		}
		return nil
	}

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

	// Remove the symlink. SUB5 #228: only escalate to the sudo-capable
	// place.RemoveSymlink when BOTH (a) state declares BinDirKindSystem,
	// AND (b) state.BinPath is actually under the configured systemBinDir.
	// Any other shape — runtime-delegated tools whose BinPath lives in the
	// runtime's bin dir (e.g., ~/go/bin/gopls), pre-SUB6 state with empty
	// BinDirKind, or a corrupted/stale state.json with an off-dir BinPath —
	// falls back to the unprivileged Placer.Cleanup. Defense in depth:
	// no path can trigger `sudo rm -f` unless it's verifiably one of ours.
	// filepath.Clean is applied because userBinDir/systemBinDir come from
	// config and may carry a trailing slash.
	if st.BinPath != "" {
		useSudoHelper := st.BinDirKindOrDefault() == resource.BinDirKindSystem &&
			filepath.Dir(st.BinPath) == filepath.Clean(i.systemBinDir)
		if useSudoHelper {
			if err := place.RemoveSymlink(ctx, st.BinPath); err != nil {
				slog.Debug("failed to remove system symlink", "path", st.BinPath, "error", err)
			}
		} else {
			if err := i.placer.Cleanup(st.BinPath); err != nil {
				slog.Debug("failed to remove symlink", "path", st.BinPath, "error", err)
			}
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
	binName := effectiveBinaryName(spec, name)
	// SHA pins this install to a git commit. The preset template
	// (e.g. "go install {{.Package}}@{{.Version}}") receives the SHA via
	// .Version, expanding to `@<sha>` without modifying the preset.
	// buildDelegationState reads spec.SHA directly to persist it separately,
	// so spec is left untouched.
	versionSlot := spec.Version
	if spec.SHA != "" {
		versionSlot = spec.SHA
	}
	vars := command.Vars{
		Package:           spec.Package.String(),
		Version:           versionSlot,
		Name:              name,
		BinPath:           filepath.Join(info.ToolBinPath, binName),
		Args:              strings.Join(spec.Args, " "),
		MinimumReleaseAge: info.MinimumReleaseAge,
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
	if err := i.executeCommand(ctx, info.Commands.Install, vars, env); err != nil {
		return nil, fmt.Errorf("failed to execute install command: %w", err)
	}

	if spec.SHA != "" {
		// SHA-pinned installs are security-relevant: surface the SHA at Info so
		// audit logs record exactly which commit was resolved.
		slog.Info("tool installed via runtime (sha-pinned)", "name", name, "sha", spec.SHA, "runtime", spec.RuntimeRef)
	} else {
		slog.Debug("tool installed via runtime", "name", name, "version", spec.Version, "runtime", spec.RuntimeRef)
	}

	// Clean up old binary when binaryName changes on upgrade/reinstall.
	// Safety: only remove if the old path is within the expected ToolBinPath directory.
	if oldBinPath := executor.OldBinPathFromContext(ctx); oldBinPath != "" && oldBinPath != vars.BinPath {
		if info.ToolBinPath != "" && filepath.Dir(oldBinPath) == info.ToolBinPath {
			slog.Debug("cleaning up old runtime binary", "old", oldBinPath, "new", vars.BinPath)
			_ = os.Remove(oldBinPath) // best-effort
		} else {
			slog.Warn("skipping old runtime binary cleanup: path is outside expected directory",
				"old", oldBinPath, "expected_dir", info.ToolBinPath)
		}
	}

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
		Package:           pkg,
		Version:           spec.Version,
		Name:              name,
		BinPath:           "", // installer manages the path
		Args:              strings.Join(spec.Args, " "),
		MinimumReleaseAge: info.MinimumReleaseAge,
	}

	// Build environment with PATH including the installer's own binDir (#269) and
	// its toolRef binary directory.
	env := i.buildEnvWithToolPath(spec.InstallerRef, info.BinDir)

	// Execute install command with output streaming
	if err := i.executeCommand(ctx, info.Commands.Install, vars, env); err != nil {
		return nil, fmt.Errorf("failed to execute install command: %w", err)
	}

	slog.Debug("tool installed via installer", "name", name, "version", spec.Version, "installer", spec.InstallerRef)

	return i.buildDelegationState(spec, ""), nil
}

// buildDelegationState creates a ToolState for delegation pattern installations.
func (i *Installer) buildDelegationState(spec *resource.ToolSpec, binPath string) *resource.ToolState {
	// SHA-pinned tools must classify as VersionExact, not VersionLatest.
	// spec.Version is empty for sha pins, so ClassifyVersion would otherwise
	// return VersionLatest and tomei's --sync / --update-tools modes would
	// taint the tool (engine.applyUpdateTaints predicates on VersionLatest).
	// A SHA pin is the strictest form of "exact" — don't surprise-reinstall.
	versionKind := resource.ClassifyVersion(spec.Version)
	if spec.SHA != "" {
		versionKind = resource.VersionExact
	}
	return &resource.ToolState{
		InstallerRef: spec.InstallerRef,
		Version:      spec.Version,
		SHA:          spec.SHA,
		VersionKind:  versionKind,
		SpecVersion:  spec.Version,
		BinPath:      binPath,
		RuntimeRef:   spec.RuntimeRef,
		Package:      spec.Package,
		BinaryName:   spec.BinaryName,
		UpdatedAt:    time.Now(),
	}
}
