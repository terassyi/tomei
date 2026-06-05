// Package aqua provides a VersionClient for fetching latest versions from GitHub.
package aqua

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/terassyi/tomei/internal/github"
)

const (
	githubAPIURL = "https://api.github.com/repos/aquaproj/aqua-registry/releases/latest"
)

// VersionClient fetches latest version information from GitHub API.
type VersionClient struct {
	httpClient *http.Client
}

// NewVersionClient creates a new VersionClient with the given HTTP client.
// If client is nil, a default HTTP client with timeout is used.
func NewVersionClient(client *http.Client) *VersionClient {
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &VersionClient{
		httpClient: client,
	}
}

// GetLatestRef fetches the latest tag of aqua-registry.
func (c *VersionClient) GetLatestRef(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubAPIURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := c.httpClient.Do(req)
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

// GetReleaseByTag fetches a specific release by tag from the GitHub
// Releases API. Used by internal/age for publication-time lookups when
// gating apply-time install on minimumReleaseAge.
// Path: /repos/{owner}/{repo}/releases/tags/{tag}.
func (c *VersionClient) GetReleaseByTag(ctx context.Context, repoOwner, repoName, tag string) (github.Release, error) {
	if err := validatePathComponent(repoOwner); err != nil {
		return github.Release{}, fmt.Errorf("invalid repo owner: %w", err)
	}
	if err := validatePathComponent(repoName); err != nil {
		return github.Release{}, fmt.Errorf("invalid repo name: %w", err)
	}
	if err := validatePathComponent(tag); err != nil {
		return github.Release{}, fmt.Errorf("invalid tag: %w", err)
	}

	apiURL := &url.URL{
		Scheme: "https",
		Host:   "api.github.com",
		Path:   path.Join("/repos", repoOwner, repoName, "releases", "tags", tag),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL.String(), nil)
	if err != nil {
		return github.Release{}, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return github.Release{}, fmt.Errorf("failed to fetch release by tag: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return github.Release{}, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var raw releaseByTagResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return github.Release{}, fmt.Errorf("failed to decode response: %w", err)
	}
	if raw.TagName == "" {
		return github.Release{}, fmt.Errorf("tag_name is empty")
	}
	return github.Release{TagName: raw.TagName, PublishedAt: raw.PublishedAt}, nil
}

// releaseByTagResponse mirrors the subset of GitHub's release-by-tag
// payload we consume.
type releaseByTagResponse struct {
	TagName     string    `json:"tag_name"`
	PublishedAt time.Time `json:"published_at"`
}

// GetLatestToolVersion fetches the latest version of the specified tool.
func (c *VersionClient) GetLatestToolVersion(ctx context.Context, repoOwner, repoName string) (string, error) {
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

	resp, err := c.httpClient.Do(req)
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
