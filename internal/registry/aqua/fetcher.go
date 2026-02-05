// Package aqua provides a Fetcher implementation for aqua-registry.
package aqua

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
)

const (
	defaultBaseURL     = "https://raw.githubusercontent.com/aquaproj/aqua-registry"
	githubAPIURL       = "https://api.github.com/repos/aquaproj/aqua-registry/releases/latest"
	defaultHTTPTimeout = 30 * time.Second
)

// Fetcher fetches package definitions from aqua-registry.
type Fetcher struct {
	cacheDir   string
	httpClient *http.Client
	baseURL    string
}

// NewFetcher creates a new Fetcher.
func NewFetcher(cacheDir string) *Fetcher {
	return &Fetcher{
		cacheDir:   cacheDir,
		httpClient: &http.Client{Timeout: defaultHTTPTimeout},
		baseURL:    defaultBaseURL,
	}
}

// WithHTTPClient sets the HTTP client (for testing).
func (f *Fetcher) WithHTTPClient(client *http.Client) *Fetcher {
	f.httpClient = client
	return f
}

// WithBaseURL sets the base URL (for testing).
func (f *Fetcher) WithBaseURL(url string) *Fetcher {
	f.baseURL = url
	return f
}

// validatePathComponent validates that a path component does not contain path traversal.
func validatePathComponent(s string) error {
	cleaned := path.Clean(s)
	if cleaned != s || strings.Contains(s, "..") || strings.HasPrefix(s, "/") {
		return fmt.Errorf("invalid path component: %s", s)
	}
	return nil
}

// cachePath constructs the cache file path with path traversal protection.
func (f *Fetcher) cachePath(ref, pkg string) (string, error) {
	if err := validatePathComponent(ref); err != nil {
		return "", fmt.Errorf("invalid ref: %w", err)
	}
	// pkg can contain one slash (e.g., "cli/cli"), validate each part
	parts := strings.Split(pkg, "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid package format: %s (expected owner/repo)", pkg)
	}
	for _, part := range parts {
		if err := validatePathComponent(part); err != nil {
			return "", fmt.Errorf("invalid package: %w", err)
		}
	}
	return filepath.Join(f.cacheDir, ref, "pkgs", pkg, "registry.yaml"), nil
}

// readCache reads package info from cache.
func (f *Fetcher) readCache(path string) (*PackageInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var info PackageInfo
	if err := yaml.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("failed to parse cached registry: %w", err)
	}

	return &info, nil
}

// buildRegistryURL constructs the registry URL with proper escaping.
func (f *Fetcher) buildRegistryURL(ref, pkg string) (string, error) {
	base, err := url.Parse(f.baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid base URL: %w", err)
	}
	// Use path.Join to safely construct the path
	base.Path = path.Join(base.Path, ref, "pkgs", pkg, "registry.yaml")
	return base.String(), nil
}

// fetchRemote fetches package definition from remote.
func (f *Fetcher) fetchRemote(ctx context.Context, ref, pkg string) ([]byte, error) {
	registryURL, err := f.buildRegistryURL(ref, pkg)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, registryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch registry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("package not found: %s", pkg)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return data, nil
}

// writeCache writes data to cache using atomic write (write to temp file then rename).
func (f *Fetcher) writeCache(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	tmpFile := path + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0o644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	if err := os.Rename(tmpFile, path); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// Fetch fetches package definition (cache-first).
func (f *Fetcher) Fetch(ctx context.Context, ref, pkg string) (*PackageInfo, error) {
	cacheFilePath, err := f.cachePath(ref, pkg)
	if err != nil {
		return nil, fmt.Errorf("failed to construct cache path: %w", err)
	}

	// 1. Check cache
	if info, err := f.readCache(cacheFilePath); err == nil {
		slog.Debug("cache hit", "package", pkg, "ref", ref)
		return info, nil
	}

	// 2. Fetch from remote
	slog.Debug("cache miss, fetching from remote", "package", pkg, "ref", ref)
	data, err := f.fetchRemote(ctx, ref, pkg)
	if err != nil {
		return nil, err
	}

	// 3. Save to cache
	if err := f.writeCache(cacheFilePath, data); err != nil {
		slog.Warn("failed to cache registry", "path", cacheFilePath, "error", err)
		// Cache failure is not fatal, continue
	}

	// 4. Parse
	var info PackageInfo
	if err := yaml.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("failed to parse registry: %w", err)
	}

	return &info, nil
}

// GetLatestRef fetches the latest tag of aqua-registry.
func (f *Fetcher) GetLatestRef(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubAPIURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if release.TagName == "" {
		return "", fmt.Errorf("tag_name is empty")
	}

	return release.TagName, nil
}

// GetLatestToolVersion fetches the latest version of the specified tool.
func (f *Fetcher) GetLatestToolVersion(ctx context.Context, repoOwner, repoName string) (string, error) {
	// Validate inputs to prevent path traversal
	if err := validatePathComponent(repoOwner); err != nil {
		return "", fmt.Errorf("invalid repo owner: %w", err)
	}
	if err := validatePathComponent(repoName); err != nil {
		return "", fmt.Errorf("invalid repo name: %w", err)
	}

	apiURL := &url.URL{
		Scheme: "https",
		Host:   "api.github.com",
		Path:   path.Join("/repos", repoOwner, repoName, "releases", "latest"),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL.String(), nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if release.TagName == "" {
		return "", fmt.Errorf("tag_name is empty")
	}

	return release.TagName, nil
}
