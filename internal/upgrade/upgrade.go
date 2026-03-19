// Package upgrade provides self-update functionality for the tomei binary.
// It queries GitHub Releases, downloads platform-specific archives, verifies checksums,
// and atomically replaces the current binary.
package upgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	semver "github.com/Masterminds/semver/v3"

	"github.com/terassyi/tomei/internal/checksum"
	"github.com/terassyi/tomei/internal/errors"
	"github.com/terassyi/tomei/internal/github"
	"github.com/terassyi/tomei/internal/installer/extract"
)

const (
	defaultAPIBaseURL      = "https://api.github.com"
	defaultDownloadBaseURL = "https://github.com"
	repoOwner              = "terassyi"
	repoName               = "tomei"
	binaryName             = "tomei"

	// StageDownloading is the progress stage for downloading the archive.
	StageDownloading = "Downloading"
	// StageChecksum is the progress stage for verifying the checksum.
	StageChecksum = "Verifying checksum"
	// StageReplacing is the progress stage for replacing the binary.
	StageReplacing = "Replacing binary"
	// StageVerifying is the progress stage for verifying the installation.
	StageVerifying = "Verifying installation"
)

// Updater manages the self-update process.
type Updater struct {
	apiClient       *http.Client
	dlClient        *http.Client
	version         string
	apiBaseURL      string
	downloadBaseURL string
}

// Option configures an Updater.
type Option func(*Updater)

// WithAPIBaseURL overrides the GitHub API base URL (for testing).
func WithAPIBaseURL(url string) Option {
	return func(u *Updater) { u.apiBaseURL = url }
}

// WithDownloadBaseURL overrides the GitHub download base URL (for testing).
func WithDownloadBaseURL(url string) Option {
	return func(u *Updater) { u.downloadBaseURL = url }
}

// CheckResult contains the result of checking for updates.
type CheckResult struct {
	CurrentVersion string
	LatestVersion  string
	UpToDate       bool
}

// Config holds flags for the upgrade check operation.
type Config struct {
	Force         bool
	TargetVersion string // empty = latest
}

// ProgressFunc is called to report progress during upgrade.
type ProgressFunc func(stage, detail string)

// NewUpdater creates an Updater with the given clients and version.
func NewUpdater(apiClient, dlClient *http.Client, version string, opts ...Option) *Updater {
	u := &Updater{
		apiClient:       apiClient,
		dlClient:        dlClient,
		version:         version,
		apiBaseURL:      defaultAPIBaseURL,
		downloadBaseURL: defaultDownloadBaseURL,
	}
	for _, o := range opts {
		o(u)
	}
	return u
}

// Check queries GitHub for the latest release and compares with the current version.
func (u *Updater) Check(ctx context.Context, cfg Config) (*CheckResult, error) {
	// Dev build guard
	if isDevBuild(u.version) && !cfg.Force && cfg.TargetVersion == "" {
		return nil, (&errors.Error{
			Category: errors.CategoryUpgrade,
			Code:     errors.CodeUpgradeBlocked,
			Message:  fmt.Sprintf("cannot upgrade from development build (version: %s)", u.version),
		}).WithHint("Use --force to override, or install a release build.")
	}

	var targetVersion string
	if cfg.TargetVersion != "" {
		targetVersion = strings.TrimPrefix(cfg.TargetVersion, "v")
		if _, err := semver.NewVersion(targetVersion); err != nil {
			return nil, (&errors.Error{
				Category: errors.CategoryUpgrade,
				Code:     errors.CodeUpgradeBlocked,
				Message:  fmt.Sprintf("invalid version %q: not a valid semver", cfg.TargetVersion),
			}).WithHint("Use a valid semver version (e.g., 0.1.3).")
		}
	} else {
		// Fetch latest release
		slog.Debug("fetching latest release", "api_base", u.apiBaseURL)
		ver, err := github.GetLatestReleaseWithBase(ctx, u.apiClient, repoOwner, repoName, "v", u.apiBaseURL)
		if err != nil {
			if isRateLimitError(err) {
				return nil, (&errors.Error{
					Category: errors.CategoryUpgrade,
					Code:     errors.CodeUpgradeBlocked,
					Message:  "GitHub API rate limit exceeded",
					Cause:    err,
				}).WithHint("Set GITHUB_TOKEN or GH_TOKEN to increase the rate limit.")
			}
			return nil, &errors.Error{
				Category: errors.CategoryUpgrade,
				Code:     errors.CodeUpgradeFailed,
				Message:  "failed to fetch latest release",
				Cause:    err,
			}
		}
		targetVersion = ver
	}

	result := &CheckResult{
		CurrentVersion: u.version,
		LatestVersion:  targetVersion,
	}

	// Compare versions (skip if --version specified)
	if cfg.TargetVersion == "" && !isDevBuild(u.version) {
		current, err := semver.NewVersion(u.version)
		if err == nil {
			latest, err := semver.NewVersion(targetVersion)
			if err == nil && !current.LessThan(latest) {
				result.UpToDate = true
			}
		}
	}

	return result, nil
}

// Upgrade downloads and installs the target version.
func (u *Updater) Upgrade(ctx context.Context, check *CheckResult, progress ProgressFunc) error {
	if check == nil {
		return &errors.Error{
			Category: errors.CategoryUpgrade,
			Code:     errors.CodeUpgradeFailed,
			Message:  "check result is nil; call Check before Upgrade",
		}
	}
	if progress == nil {
		progress = func(_, _ string) {}
	}

	// Platform support check
	if err := checkPlatformSupport(runtime.GOOS, runtime.GOARCH); err != nil {
		return err
	}

	// Resolve binary path
	binaryPath, err := resolveBinaryPath()
	if err != nil {
		return err
	}

	// Pre-flight writability check
	if err := checkWritable(filepath.Dir(binaryPath)); err != nil {
		return (&errors.Error{
			Category: errors.CategoryUpgrade,
			Code:     errors.CodeUpgradeBlocked,
			Message:  fmt.Sprintf("permission denied: %s", binaryPath),
			Cause:    err,
		}).WithHint("Run with elevated privileges, or reinstall to ~/.local/bin/")
	}

	// Package manager warning
	warnIfPackageManaged(binaryPath)

	ver := check.LatestVersion
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	archive := archiveName(ver, goos, goarch)

	// Create temp dir
	tmpDir, err := os.MkdirTemp("", "tomei-upgrade-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Download archive
	archiveURL := releaseAssetURL(u.downloadBaseURL, ver, archive)
	archivePath := filepath.Join(tmpDir, archive)
	progress(StageDownloading, fmt.Sprintf("tomei v%s (%s/%s)...", ver, goos, goarch))
	slog.Debug("downloading archive", "url", archiveURL)
	if err := downloadFile(ctx, u.dlClient, archiveURL, archivePath); err != nil {
		return &errors.Error{
			Category: errors.CategoryUpgrade,
			Code:     errors.CodeUpgradeFailed,
			Message:  "failed to download release archive",
			Cause:    err,
		}
	}

	// Download and verify checksum
	checksumURL := releaseAssetURL(u.downloadBaseURL, ver, "checksums.txt")
	progress(StageChecksum, "")
	slog.Debug("downloading checksums", "url", checksumURL)
	checksumBody, err := fetchBody(ctx, u.dlClient, checksumURL)
	if err != nil {
		return &errors.Error{
			Category: errors.CategoryUpgrade,
			Code:     errors.CodeUpgradeFailed,
			Message:  "failed to download checksums file",
			Cause:    err,
		}
	}
	algo, digest, err := checksum.ParseFile(checksumBody, archive)
	if err != nil {
		return &errors.Error{
			Category: errors.CategoryUpgrade,
			Code:     errors.CodeUpgradeFailed,
			Message:  "failed to parse checksums file",
			Cause:    err,
		}
	}
	if err := checksum.Verify(archivePath, algo, digest); err != nil {
		return &errors.Error{
			Category: errors.CategoryUpgrade,
			Code:     errors.CodeUpgradeFailed,
			Message:  "checksum verification failed",
			Cause:    err,
		}
	}

	// Extract
	extractDir := filepath.Join(tmpDir, "extract")
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return fmt.Errorf("failed to create extract directory: %w", err)
	}
	ext, err := extract.NewExtractor(extract.ArchiveTypeTarGz)
	if err != nil {
		return fmt.Errorf("failed to create extractor: %w", err)
	}
	archiveFile, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}
	defer archiveFile.Close()
	if err := ext.Extract(archiveFile, extractDir); err != nil {
		return &errors.Error{
			Category: errors.CategoryUpgrade,
			Code:     errors.CodeUpgradeFailed,
			Message:  "failed to extract archive",
			Cause:    err,
		}
	}

	// Find binary in extracted archive (GoReleaser flat archive)
	newBinaryPath, err := findBinary(extractDir, binaryName)
	if err != nil {
		return &errors.Error{
			Category: errors.CategoryUpgrade,
			Code:     errors.CodeUpgradeFailed,
			Message:  "binary not found in archive",
			Cause:    err,
		}
	}

	// Replace binary
	progress(StageReplacing, "")
	if err := replaceBinary(binaryPath, newBinaryPath); err != nil {
		return &errors.Error{
			Category: errors.CategoryUpgrade,
			Code:     errors.CodeUpgradeFailed,
			Message:  "failed to replace binary",
			Cause:    err,
		}
	}

	// Verify installation
	progress(StageVerifying, "")
	if err := verifyBinary(ctx, binaryPath, ver); err != nil {
		return &errors.Error{
			Category: errors.CategoryUpgrade,
			Code:     errors.CodeUpgradeFailed,
			Message:  "verification of new binary failed",
			Cause:    err,
			Hint:     "The binary was replaced but version verification failed. You may need to reinstall.",
		}
	}

	return nil
}

// resolveBinaryPath returns the real path of the current executable, resolving symlinks.
func resolveBinaryPath() (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", &errors.Error{
			Category: errors.CategoryUpgrade,
			Code:     errors.CodeUpgradeFailed,
			Message:  "failed to determine executable path",
			Cause:    err,
		}
	}
	resolved, err := filepath.EvalSymlinks(execPath)
	if err != nil {
		return "", &errors.Error{
			Category: errors.CategoryUpgrade,
			Code:     errors.CodeUpgradeFailed,
			Message:  "failed to resolve executable symlinks",
			Cause:    err,
		}
	}
	return resolved, nil
}

// isDevBuild returns true if the version string is not a valid semver.
func isDevBuild(version string) bool {
	if version == "" {
		return true
	}
	_, err := semver.NewVersion(version)
	return err != nil
}

// archiveName constructs the archive filename for a release.
func archiveName(version, goos, goarch string) string {
	return fmt.Sprintf("tomei_v%s_%s_%s.tar.gz", version, goos, goarch)
}

// releaseAssetURL constructs the URL for a release asset.
func releaseAssetURL(baseURL, version, filename string) string {
	return fmt.Sprintf("%s/%s/%s/releases/download/v%s/%s", baseURL, repoOwner, repoName, version, filename)
}

// replaceBinary atomically replaces the current binary with a new one.
// It creates a backup, copies the new binary via a temp file, and does an atomic rename.
// The original file's permissions are preserved. On function error or panic, the backup
// is restored via defer. Note: this does not protect against SIGKILL or power loss.
func replaceBinary(currentPath, newBinaryPath string) error {
	// Preserve original permissions
	origInfo, err := os.Stat(currentPath)
	if err != nil {
		return fmt.Errorf("failed to stat current binary: %w", err)
	}
	origMode := origInfo.Mode().Perm()

	dir := filepath.Dir(currentPath)

	// Reserve a unique backup path via CreateTemp (O_EXCL).
	// The temp file is kept so the path stays reserved; Rename overwrites it atomically on Unix.
	backupFile, err := os.CreateTemp(dir, filepath.Base(currentPath)+".bak.*")
	if err != nil {
		return fmt.Errorf("failed to create backup file: %w", err)
	}
	backupPath := backupFile.Name()
	backupFile.Close()

	var backupCreated, upgraded bool
	defer func() {
		if backupCreated && !upgraded {
			slog.Debug("restoring from backup", "backup", backupPath, "target", currentPath)
			if err := os.Rename(backupPath, currentPath); err != nil {
				slog.Error("failed to restore from backup", "error", err)
			}
		}
		if upgraded {
			os.Remove(backupPath)
		}
	}()

	// Move current binary to backup
	if err := os.Rename(currentPath, backupPath); err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}
	backupCreated = true

	// Copy new binary to a temp staging file.
	// Write directly into the fd returned by CreateTemp to avoid TOCTOU.
	stagingFile, err := os.CreateTemp(dir, filepath.Base(currentPath)+".new.*")
	if err != nil {
		return fmt.Errorf("failed to create staging file: %w", err)
	}
	stagingPath := stagingFile.Name()

	if err := copyToWriter(newBinaryPath, stagingFile); err != nil {
		stagingFile.Close()
		os.Remove(stagingPath)
		return fmt.Errorf("failed to copy new binary: %w", err)
	}
	if err := stagingFile.Close(); err != nil {
		os.Remove(stagingPath)
		return fmt.Errorf("failed to close staging file: %w", err)
	}

	// Atomic rename into place
	if err := os.Rename(stagingPath, currentPath); err != nil {
		os.Remove(stagingPath)
		return fmt.Errorf("failed to install new binary: %w", err)
	}

	// Restore original permissions
	if err := os.Chmod(currentPath, origMode); err != nil {
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	upgraded = true
	return nil
}

// verifyBinary executes the installed binary with "version --output json" and
// confirms the reported version matches the expected version.
func verifyBinary(ctx context.Context, binaryPath, expectedVersion string) error {
	cmd := exec.CommandContext(ctx, binaryPath, "version", "--output", "json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run version check: %w\noutput: %s", err, out)
	}

	var info struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(out, &info); err != nil {
		return fmt.Errorf("failed to parse version output: %w", err)
	}

	if info.Version != expectedVersion {
		return fmt.Errorf("version mismatch: expected %s, got %s", expectedVersion, info.Version)
	}

	return nil
}

// findBinary walks the extract directory to find the named binary.
// GoReleaser creates flat archives (binary at root), so we check the root first,
// then walk subdirectories.
func findBinary(extractDir, name string) (string, error) {
	// Check root level first (flat archive)
	candidate := filepath.Join(extractDir, name)
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate, nil
	}

	// Walk one level of subdirectories
	entries, err := os.ReadDir(extractDir)
	if err != nil {
		return "", fmt.Errorf("failed to read extract directory: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		candidate = filepath.Join(extractDir, e.Name(), name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("binary %q not found in extracted archive", name)
}

// checkWritable tests whether the directory is writable by creating and removing a temp file.
func checkWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".tomei-upgrade-*")
	if err != nil {
		return err
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		os.Remove(name)
		return err
	}
	return os.Remove(name)
}

// supportedPlatforms lists the GOOS/GOARCH combinations that have release builds.
var supportedPlatforms = map[string]bool{
	"linux/amd64":  true,
	"linux/arm64":  true,
	"darwin/arm64": true,
}

// checkPlatformSupport returns an error if the current platform has no release builds.
func checkPlatformSupport(goos, goarch string) error {
	platform := goos + "/" + goarch
	if supportedPlatforms[platform] {
		return nil
	}
	return (&errors.Error{
		Category: errors.CategoryUpgrade,
		Code:     errors.CodeUpgradeBlocked,
		Message:  fmt.Sprintf("unsupported platform: %s", platform),
	}).WithHint("Supported platforms: linux/amd64, linux/arm64, darwin/arm64.")
}

// warnIfPackageManaged logs a warning if the binary appears to be managed by a package manager.
func warnIfPackageManaged(binaryPath string) {
	prefixes := []string{"/opt/homebrew/", "/usr/local/Cellar/", "/home/linuxbrew/"}
	for _, p := range prefixes {
		if strings.HasPrefix(binaryPath, p) {
			slog.Warn("binary appears to be managed by Homebrew; consider using brew upgrade instead", "path", binaryPath)
			return
		}
	}
}

// isRateLimitError detects if an error is due to GitHub API rate limiting.
// This is a heuristic based on HTTP 403 from github.GetLatestReleaseWithBase;
// 403 can also mean other access issues, but rate limiting is the most common cause
// for public repositories.
func isRateLimitError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "status 403")
}

// validateURL checks that the URL uses HTTPS (or http://localhost for testing).
func validateURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", rawURL, err)
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" {
		host := u.Hostname()
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return nil
		}
	}
	return fmt.Errorf("URL scheme %q is not allowed; use HTTPS: %s", u.Scheme, rawURL)
}

// downloadFile downloads a URL to a local file.
func downloadFile(ctx context.Context, client *http.Client, url, destPath string) error {
	if err := validateURL(url); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s returned status %d", url, resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return fmt.Errorf("failed to write file: %w", err)
	}

	return f.Close()
}

// fetchBody downloads a URL and returns the response body as bytes.
func fetchBody(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	if err := validateURL(url); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch failed: %s returned status %d", url, resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// copyToWriter copies the contents of srcPath into an already-open writer.
func copyToWriter(srcPath string, w io.Writer) error {
	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()
	_, err = io.Copy(w, in)
	return err
}

// copyFile copies src to dst, preserving permissions.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}

	// Preserve source permissions
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.Chmod(dst, info.Mode())
}
