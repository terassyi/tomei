// Package aqua provides a fetcher implementation for aqua-registry.
package aqua

import (
	"context"
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
	defaultHTTPTimeout = 30 * time.Second
)

// fetcher fetches package definitions from aqua-registry.
type fetcher struct {
	cacheDir   string
	httpClient *http.Client
	baseURL    string
}

// newFetcher creates a new fetcher.
func newFetcher(cacheDir string) *fetcher {
	return &fetcher{
		cacheDir:   cacheDir,
		httpClient: &http.Client{Timeout: defaultHTTPTimeout},
		baseURL:    defaultBaseURL,
	}
}

// withHTTPClient sets the HTTP client (for testing).
func (f *fetcher) withHTTPClient(client *http.Client) *fetcher {
	f.httpClient = client
	return f
}

// withBaseURL sets the base URL (for testing).
func (f *fetcher) withBaseURL(url string) *fetcher {
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
func (f *fetcher) cachePath(ref, pkg string) (string, error) {
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

// registryFile represents the structure of registry.yaml file.
// The file contains a list of packages under the "packages" key.
type registryFile struct {
	Packages []PackageInfo `yaml:"packages"`
}

// readCache reads package info from cache.
func (f *fetcher) readCache(path string) (*PackageInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return parseRegistryYAML(data)
}

// parseRegistryYAML parses registry.yaml content and returns the first package.
func parseRegistryYAML(data []byte) (*PackageInfo, error) {
	var file registryFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("failed to parse registry: %w", err)
	}

	if len(file.Packages) == 0 {
		return nil, fmt.Errorf("no packages found in registry.yaml")
	}

	// Return the first package (each registry.yaml typically contains one package)
	return &file.Packages[0], nil
}

// buildRegistryURL constructs the registry URL with proper escaping.
func (f *fetcher) buildRegistryURL(ref, pkg string) (string, error) {
	base, err := url.Parse(f.baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid base URL: %w", err)
	}
	// Use path.Join to safely construct the path
	base.Path = path.Join(base.Path, ref, "pkgs", pkg, "registry.yaml")
	return base.String(), nil
}

// fetchRemote fetches package definition from remote.
func (f *fetcher) fetchRemote(ctx context.Context, ref, pkg string) ([]byte, error) {
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
func (f *fetcher) writeCache(path string, data []byte) error {
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

// fetch fetches package definition (cache-first).
func (f *fetcher) fetch(ctx context.Context, ref, pkg string) (*PackageInfo, error) {
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
	return parseRegistryYAML(data)
}
