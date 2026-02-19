package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// releaseResponse represents a subset of the GitHub Releases API response.
type releaseResponse struct {
	TagName string `json:"tag_name"`
}

// GetLatestRelease fetches the latest release tag from a GitHub repository.
// It strips the optional tagPrefix from the tag name (e.g., "bun-v" from "bun-v1.2.3").
// Returns the version string without the prefix.
func GetLatestRelease(ctx context.Context, client *http.Client, owner, repo, tagPrefix string) (string, error) {
	if strings.Contains(owner, "/") || strings.Contains(repo, "/") {
		return "", fmt.Errorf("invalid owner %q or repo %q: must not contain '/'", owner, repo)
	}
	if owner == "" || repo == "" {
		return "", fmt.Errorf("owner and repo must not be empty")
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned status %d for %s/%s", resp.StatusCode, owner, repo)
	}

	var release releaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if release.TagName == "" {
		return "", fmt.Errorf("empty tag_name in latest release for %s/%s", owner, repo)
	}

	version := strings.TrimPrefix(release.TagName, tagPrefix)
	return version, nil
}
