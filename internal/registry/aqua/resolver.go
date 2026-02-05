// Package aqua provides a Resolver that resolves aqua-registry packages to download URLs.
package aqua

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"strings"
)

// ResolvedSource contains the resolved download information for a package.
//
// Example output for cli/cli v2.86.0 on darwin/arm64:
//
//	ResolvedSource{
//	    URL:         "https://github.com/cli/cli/releases/download/v2.86.0/gh_2.86.0_macOS_arm64.tar.gz",
//	    ChecksumURL: "https://github.com/cli/cli/releases/download/v2.86.0/gh_2.86.0_checksums.txt",
//	    Algorithm:   "sha256",
//	    Format:      "tar.gz",
//	    Files:       nil,
//	    Warnings:    [],
//	    Errors:      [],
//	}
type ResolvedSource struct {
	// URL is the download URL for the package asset.
	// Example: "https://github.com/cli/cli/releases/download/v2.86.0/gh_2.86.0_macOS_arm64.tar.gz"
	URL string

	// ChecksumURL is the URL of the checksum file (empty if not available).
	// Example: "https://github.com/cli/cli/releases/download/v2.86.0/gh_2.86.0_checksums.txt"
	ChecksumURL string

	// Algorithm is the checksum algorithm (e.g., "sha256", "sha512").
	// Defaults to "sha256" if not specified in the registry.
	Algorithm string

	// Format is the archive format (e.g., "tar.gz", "zip", "raw").
	Format string

	// Files specifies which files to extract from the archive.
	// If empty, all files are extracted.
	Files []FileSpec

	// Warnings contains non-fatal issues detected during resolution.
	// These should be displayed to the user during `toto plan`.
	// Example: "version v1.5.0 uses legacy asset format"
	Warnings []string

	// Errors contains fatal issues that prevent installation.
	// If non-empty, the package cannot be installed.
	// Example: "package example/tool does not support windows/amd64"
	Errors []string
}

// Resolver resolves aqua-registry packages to download URLs.
//
// It performs the following steps:
//  1. Fetch package definition (registry.yaml) from aqua-registry
//  2. Check if the current OS/Arch is supported (supported_envs)
//  3. Apply version-specific overrides (version_overrides)
//  4. Apply OS-specific overrides (overrides)
//  5. Apply replacements (e.g., amd64 → x86_64, darwin → macOS)
//  6. Render asset template to build the final download URL
//
// Usage:
//
//	resolver := aqua.NewResolver(cacheDir)
//	result, err := resolver.Resolve(ctx, "v4.465.0", "cli/cli", "v2.86.0")
//	// result.URL: "https://github.com/cli/cli/releases/download/v2.86.0/gh_2.86.0_macOS_arm64.tar.gz"
type Resolver struct {
	fetcher       *fetcher
	versionClient *VersionClient
}

// NewResolver creates a new Resolver with the specified cache directory.
//
// The cache directory is used to store fetched registry.yaml files.
// Files are cached per registry ref (e.g., ~/.cache/toto/registry/aqua/v4.465.0/pkgs/cli/cli/registry.yaml).
func NewResolver(cacheDir string) *Resolver {
	f := newFetcher(cacheDir)
	return &Resolver{
		fetcher:       f,
		versionClient: newVersionClientWithHTTPClient(f.httpClient),
	}
}

// NewResolverWithBaseURL creates a new Resolver with a custom base URL.
// This is primarily for testing with mock HTTP servers.
func NewResolverWithBaseURL(cacheDir, baseURL string) *Resolver {
	f := newFetcher(cacheDir).withBaseURL(baseURL)
	return &Resolver{
		fetcher:       f,
		versionClient: newVersionClientWithHTTPClient(f.httpClient),
	}
}

// WithHTTPClient sets the HTTP client (for testing).
func (r *Resolver) WithHTTPClient(client *http.Client) *Resolver {
	r.fetcher = r.fetcher.withHTTPClient(client)
	r.versionClient = newVersionClientWithHTTPClient(client)
	return r
}

// WithBaseURL sets the base URL for fetching registry files (for testing).
func (r *Resolver) WithBaseURL(url string) *Resolver {
	r.fetcher = r.fetcher.withBaseURL(url)
	return r
}

// VersionClient returns the VersionClient for fetching latest versions.
//
// Usage:
//
//	ref, _ := resolver.VersionClient().GetLatestRef(ctx)
//	version, _ := resolver.VersionClient().GetLatestToolVersion(ctx, "cli", "cli")
func (r *Resolver) VersionClient() *VersionClient {
	return r.versionClient
}

// FetchPackageInfo fetches package metadata from aqua-registry.
// This is useful for getting repo_owner/repo_name to query latest version.
//
// Parameters:
//   - ref: aqua-registry version (e.g., "v4.465.0")
//   - pkg: package name in "owner/repo" format (e.g., "cli/cli")
func (r *Resolver) FetchPackageInfo(ctx context.Context, ref RegistryRef, pkg string) (*PackageInfo, error) {
	return r.fetcher.fetch(ctx, string(ref), pkg)
}

// Resolve resolves a package to its download URL and metadata.
//
// Parameters:
//   - ref: aqua-registry version (e.g., "v4.465.0")
//   - pkg: package name in "owner/repo" format (e.g., "cli/cli")
//   - version: tool version (e.g., "v2.86.0")
//
// Returns ResolvedSource with the download URL and metadata.
// Check result.Errors before using the URL - if non-empty, installation is not possible.
func (r *Resolver) Resolve(ctx context.Context, ref RegistryRef, pkg, version string) (*ResolvedSource, error) {
	return r.ResolveWithOS(ctx, ref, pkg, version, runtime.GOOS, runtime.GOARCH)
}

// ResolveWithOS resolves a package with explicit OS and Arch.
// This is primarily for testing - use Resolve() for normal usage.
func (r *Resolver) ResolveWithOS(ctx context.Context, ref RegistryRef, pkg, version, goos, goarch string) (*ResolvedSource, error) {
	// 1. Fetch package info from aqua-registry (cache-first)
	info, err := r.fetcher.fetch(ctx, string(ref), pkg)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch package info: %w", err)
	}

	result := &ResolvedSource{
		Warnings: []string{},
		Errors:   []string{},
	}

	// 2. Check supported_envs - early return if current OS/Arch is not supported
	if len(info.SupportedEnvs) > 0 {
		env := fmt.Sprintf("%s/%s", goos, goarch)
		if !isSupportedEnv(info.SupportedEnvs, goos, goarch) {
			result.Errors = append(result.Errors,
				fmt.Sprintf("package %s does not support %s (supported: %s)",
					pkg, env, strings.Join(info.SupportedEnvs, ", ")))
			return result, nil
		}
	}

	// 3. Apply version overrides (e.g., old versions may have different asset format)
	originalAsset := info.Asset
	info = ApplyVersionOverrides(info, version)
	if info.Asset != originalAsset {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("version %s uses legacy asset format", version))
	}

	// 4. Apply OS overrides (e.g., Windows uses .zip instead of .tar.gz)
	info = applyOSOverrides(info, goos, goarch)

	// 5. Apply replacements (e.g., amd64 → x86_64, darwin → macOS)
	osName := applyReplacement(info.Replacements, goos)
	archName := applyReplacement(info.Replacements, goarch)

	// 6. Build template variables for asset name rendering
	vars := TemplateVars{
		Version: version,
		SemVer:  strings.TrimPrefix(version, "v"),
		OS:      osName,
		Arch:    archName,
		Format:  info.Format,
	}

	// 7. Render asset name first (needed for checksum templates like "{{.Asset}}.sha256")
	if info.Asset != "" {
		renderedAsset, err := RenderTemplate(info.Asset, vars)
		if err != nil {
			return nil, fmt.Errorf("failed to render asset template: %w", err)
		}
		vars.Asset = renderedAsset
	}

	// 8. Build download URL from template
	url, err := r.buildURL(info, vars)
	if err != nil {
		return nil, fmt.Errorf("failed to build URL: %w", err)
	}
	result.URL = url

	// 9. Build checksum URL if available
	if info.Checksum != nil && info.Checksum.Asset != "" {
		checksumURL, err := r.buildChecksumURL(info, vars)
		if err != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("failed to build checksum URL: %v", err))
		} else {
			result.ChecksumURL = checksumURL
			result.Algorithm = info.Checksum.Algorithm
			if result.Algorithm == "" {
				result.Algorithm = "sha256"
			}
		}
	}

	// 10. Set format and files
	result.Format = info.Format
	result.Files = info.Files

	return result, nil
}

// buildURL constructs the download URL based on package type.
//
// Supported types:
//   - "github_release": https://github.com/{owner}/{repo}/releases/download/{version}/{asset}
//   - "http": arbitrary URL with template variables
func (r *Resolver) buildURL(info *PackageInfo, vars TemplateVars) (string, error) {
	switch info.Type {
	case "github_release":
		asset, err := RenderTemplate(info.Asset, vars)
		if err != nil {
			return "", fmt.Errorf("failed to render asset template: %w", err)
		}
		return fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s",
			info.RepoOwner, info.RepoName, vars.Version, asset), nil

	case "http":
		return RenderTemplate(info.URL, vars)

	default:
		return "", fmt.Errorf("unsupported package type: %s", info.Type)
	}
}

// buildChecksumURL constructs the checksum file URL.
func (r *Resolver) buildChecksumURL(info *PackageInfo, vars TemplateVars) (string, error) {
	if info.Checksum == nil {
		return "", nil
	}

	checksumAsset, err := RenderTemplate(info.Checksum.Asset, vars)
	if err != nil {
		return "", fmt.Errorf("failed to render checksum asset template: %w", err)
	}

	switch info.Checksum.Type {
	case "github_release", "":
		return fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s",
			info.RepoOwner, info.RepoName, vars.Version, checksumAsset), nil
	default:
		return "", fmt.Errorf("unsupported checksum type: %s", info.Checksum.Type)
	}
}

// isSupportedEnv checks if the given OS/Arch is in the supported environments list.
//
// Supported formats in aqua-registry:
//   - "all": matches any environment
//   - "linux", "darwin", "windows": matches any arch on the specified OS
//   - "linux/amd64", "darwin/arm64": matches specific OS/Arch combination
func isSupportedEnv(supportedEnvs []string, goos, goarch string) bool {
	env := fmt.Sprintf("%s/%s", goos, goarch)

	for _, supported := range supportedEnvs {
		if supported == "all" {
			return true
		}
		if supported == goos {
			return true
		}
		if supported == env {
			return true
		}
	}
	return false
}
