package runtime

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/terassyi/tomei/internal/checksum"
	"github.com/terassyi/tomei/internal/github"
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
	httpClient       *http.Client
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

	// Resolve version if configured
	resolvedVersion, versionKind, err := i.resolveVersionValue(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve version: %w", err)
	}

	// Expand {{.Version}} templates in URL
	sourceURL, err := expandVersionTemplate(spec.Source.URL, resolvedVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to expand source URL template: %w", err)
	}

	// Expand {{.Version}} in checksum URL if present
	var checksumSpec *resource.Checksum
	if spec.Source.Checksum != nil {
		checksumSpec = &resource.Checksum{
			Value:       spec.Source.Checksum.Value,
			FilePattern: spec.Source.Checksum.FilePattern,
		}
		if spec.Source.Checksum.URL != "" {
			checksumURL, err := expandVersionTemplate(spec.Source.Checksum.URL, resolvedVersion)
			if err != nil {
				return nil, fmt.Errorf("failed to expand checksum URL template: %w", err)
			}
			checksumSpec.URL = checksumURL
		}
	}

	// Calculate install path using resolved version
	installPath := filepath.Join(i.runtimesDir, name, resolvedVersion)

	// Check if already installed
	if _, err := os.Stat(installPath); err == nil {
		slog.Debug("runtime already installed, rebuilding symlinks", "name", name, "version", resolvedVersion)
		binDir, err := i.resolveBinDir(spec)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve bin directory: %w", err)
		}
		if binDir != "" && len(spec.Binaries) > 0 {
			if err := i.createSymlinks(installPath, spec.Binaries, binDir); err != nil {
				return nil, fmt.Errorf("failed to rebuild symlinks: %w", err)
			}
		}
		return i.buildStateResolved(spec, installPath, binDir, resolvedVersion, versionKind), nil
	}

	// Download
	tmpDir, err := os.MkdirTemp("", "tomei-runtime-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	urlFilename := filepath.Base(sourceURL)
	archivePath := filepath.Join(tmpDir, urlFilename)
	// Prefer context callback for parallel execution
	progressCb := download.CallbackFromContext[download.ProgressCallback](ctx)
	if progressCb == nil {
		progressCb = i.progressCallback
	}
	_, err = i.downloader.DownloadWithProgress(ctx, sourceURL, archivePath, progressCb)
	if err != nil {
		return nil, fmt.Errorf("failed to download: %w", err)
	}

	// Verify checksum
	if err := i.downloader.Verify(ctx, archivePath, checksumSpec); err != nil {
		return nil, fmt.Errorf("failed to verify checksum: %w", err)
	}

	// Determine archive type
	archiveType := spec.Source.ArchiveType
	if archiveType == "" {
		archiveType = extract.DetectArchiveType(sourceURL)
		if archiveType == "" {
			return nil, fmt.Errorf("cannot determine archive type from URL: %s", sourceURL)
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

	slog.Debug("runtime installed successfully", "name", name, "version", resolvedVersion, "path", installPath)

	return i.buildStateResolved(spec, installPath, binDir, resolvedVersion, versionKind), nil
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

// buildStateResolved creates a RuntimeState with explicit resolved version and version kind.
func (i *Installer) buildStateResolved(spec *resource.RuntimeSpec, installPath, binDir, resolvedVersion string, versionKind resource.VersionKind) *resource.RuntimeState {
	digest := ""
	if spec.Source != nil && spec.Source.Checksum != nil {
		digest = checksum.ExtractHash(spec.Source.Checksum)
	}

	// Expand ~ in toolBinPath
	toolBinPath := spec.ToolBinPath
	if expanded, err := path.Expand(toolBinPath); err == nil {
		toolBinPath = expanded
	}

	// Expand {{.Version}} templates in env values, then expand ~
	env := make(map[string]string)
	for k, v := range spec.Env {
		expanded, err := expandVersionTemplate(v, resolvedVersion)
		if err != nil {
			expanded = v
		}
		if pathExpanded, err := path.Expand(expanded); err == nil {
			env[k] = pathExpanded
		} else {
			env[k] = expanded
		}
	}

	return &resource.RuntimeState{
		Type:           spec.Type,
		Version:        resolvedVersion,
		VersionKind:    versionKind,
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

// resolveVersionValue resolves the runtime version using resolveVersion commands.
// If resolveVersion is not configured or the version is an exact version number,
// returns the spec version as-is.
// Supports "github-release:owner/repo:tagPrefix" and "http-text:URL:regex"
// built-in syntaxes, and shell commands as fallback.
func (i *Installer) resolveVersionValue(ctx context.Context, spec *resource.RuntimeSpec) (string, resource.VersionKind, error) {
	// No resolveVersion configured — use spec version directly
	if len(spec.ResolveVersion) == 0 {
		return spec.Version, resource.ClassifyVersion(spec.Version), nil
	}

	// Exact version specified — skip resolution even if resolveVersion is configured
	if resource.IsExactVersion(spec.Version) {
		return spec.Version, resource.VersionExact, nil
	}

	cmd := spec.ResolveVersion[0]

	// Built-in GitHub release resolver
	if strings.HasPrefix(cmd, "github-release:") {
		version, err := i.resolveGitHubRelease(ctx, cmd)
		if err != nil {
			return "", "", err
		}
		return version, resource.VersionAlias, nil
	}

	// Built-in HTTP text resolver
	if strings.HasPrefix(cmd, "http-text:") {
		version, err := i.resolveHTTPText(ctx, cmd)
		if err != nil {
			return "", "", err
		}
		return version, resource.VersionAlias, nil
	}

	// Shell command fallback
	slog.Debug("resolving version via command", "command", cmd)
	version, err := i.cmdExecutor.ExecuteCapture(ctx, spec.ResolveVersion, command.Vars{Version: spec.Version}, nil)
	if err != nil {
		return "", "", err
	}
	if version == "" {
		return "", "", fmt.Errorf("resolveVersion command returned empty result")
	}

	slog.Debug("version resolved via command", "resolved", version)
	return version, resource.VersionAlias, nil
}

// resolveGitHubRelease parses "github-release:owner/repo:tagPrefix" and fetches the latest release.
func (i *Installer) resolveGitHubRelease(ctx context.Context, cmd string) (string, error) {
	// Parse "github-release:owner/repo:tagPrefix"
	rest := strings.TrimPrefix(cmd, "github-release:")
	parts := strings.SplitN(rest, ":", 2)

	ownerRepo := parts[0]
	tagPrefix := ""
	if len(parts) == 2 {
		tagPrefix = parts[1]
	}

	owner, repo, ok := strings.Cut(ownerRepo, "/")
	if !ok || owner == "" || repo == "" {
		return "", fmt.Errorf("invalid github-release format %q: expected github-release:owner/repo[:tagPrefix]", cmd)
	}

	client := i.httpClient
	if client == nil {
		client = github.NewHTTPClient(github.TokenFromEnv())
	}

	slog.Debug("resolving version via GitHub release", "owner", owner, "repo", repo, "tagPrefix", tagPrefix)
	version, err := github.GetLatestRelease(ctx, client, owner, repo, tagPrefix)
	if err != nil {
		return "", fmt.Errorf("failed to resolve GitHub release: %w", err)
	}
	if version == "" {
		return "", fmt.Errorf("GitHub release returned empty version for %s/%s", owner, repo)
	}

	slog.Debug("version resolved via GitHub release", "owner", owner, "repo", repo, "version", version)
	return version, nil
}

// resolveHTTPText parses "http-text:<URL>:<regex>" and fetches the URL,
// then applies the regex to extract a version from the response body.
// The URL and regex are separated by the last ":" after the "://" scheme separator.
// Note: the regex portion must not contain literal ":" characters, as LastIndex
// is used to split the URL from the regex.
func (i *Installer) resolveHTTPText(ctx context.Context, cmd string) (string, error) {
	// Strip the "http-text:" prefix
	rest := strings.TrimPrefix(cmd, "http-text:")

	// Find the scheme separator "://" to avoid splitting on it
	schemeIdx := strings.Index(rest, "://")
	if schemeIdx < 0 {
		return "", fmt.Errorf("invalid http-text format %q: missing ://", cmd)
	}

	// Find the last ":" after the scheme — this separates URL from regex
	afterScheme := rest[schemeIdx+3:]
	lastColon := strings.LastIndex(afterScheme, ":")
	if lastColon < 0 {
		return "", fmt.Errorf("invalid http-text format %q: expected http-text:<URL>:<regex>", cmd)
	}

	url := rest[:schemeIdx+3+lastColon]
	pattern := afterScheme[lastColon+1:]

	if url == "" || pattern == "" {
		return "", fmt.Errorf("invalid http-text format %q: URL and regex must not be empty", cmd)
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid http-text regex %q: %w", pattern, err)
	}

	slog.Debug("resolving version via http-text", "url", url, "regex", pattern)

	client := i.httpClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http-text: %s returned status %d", url, resp.StatusCode)
	}

	// Read body (limit to 1 MiB to prevent abuse)
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Scan line by line for the first match
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		m := re.FindStringSubmatch(line)
		if m != nil {
			if len(m) > 1 {
				// Return first capture group
				slog.Debug("version resolved via http-text", "version", m[1])
				return m[1], nil
			}
			// No capture group — return full match
			slog.Debug("version resolved via http-text", "version", m[0])
			return m[0], nil
		}
	}

	return "", fmt.Errorf("http-text: no match for regex %q in response from %s", pattern, url)
}

// versionVars holds template variables for version template expansion.
type versionVars struct {
	Version string
}

// expandVersionTemplate expands {{.Version}} in a template string.
// If the string contains no template markers, it is returned as-is.
func expandVersionTemplate(tmpl, version string) (string, error) {
	if !strings.Contains(tmpl, "{{") {
		return tmpl, nil
	}

	t, err := template.New("version").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("failed to parse version template %q: %w", tmpl, err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, versionVars{Version: version}); err != nil {
		return "", fmt.Errorf("failed to execute version template: %w", err)
	}

	return buf.String(), nil
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
