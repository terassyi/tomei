// Package github provides GitHub-aware HTTP client with token authentication.
//
// It reads GITHUB_TOKEN or GH_TOKEN from environment variables and creates
// an http.Client that automatically adds Authorization headers to requests
// for GitHub hosts. This increases the GitHub API rate limit from 60 to 5,000
// requests per hour and enables access to private repositories.
package github

import (
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultTimeout = 30 * time.Second

	// envGitHubToken is the primary environment variable for GitHub token.
	envGitHubToken = "GITHUB_TOKEN"
	// envGHToken is the fallback environment variable for GitHub token (used by gh CLI).
	envGHToken = "GH_TOKEN"

	// hostGitHub is the main GitHub domain.
	hostGitHub = "github.com"
	// hostGitHubAPI is the GitHub API domain.
	hostGitHubAPI = "api.github.com"
	// suffixGitHub is the suffix for GitHub subdomains (e.g., uploads.github.com).
	suffixGitHub = ".github.com"
	// suffixGitHubusercontent is the suffix for GitHub content delivery domains
	// (e.g., raw.githubusercontent.com, objects.githubusercontent.com).
	suffixGitHubusercontent = ".githubusercontent.com"
)

// TokenFromEnv reads GITHUB_TOKEN or GH_TOKEN from environment.
// GITHUB_TOKEN takes precedence. Returns empty string if neither is set.
func TokenFromEnv() string {
	if t := os.Getenv(envGitHubToken); t != "" {
		return t
	}
	return os.Getenv(envGHToken)
}

// NewHTTPClient creates an http.Client that adds Authorization header
// to requests for GitHub hosts (api.github.com, github.com,
// *.githubusercontent.com).
// If token is empty, returns a plain client with timeout.
func NewHTTPClient(token string) *http.Client {
	return &http.Client{
		Timeout: defaultTimeout,
		Transport: &tokenTransport{
			token: token,
			base:  http.DefaultTransport,
		},
	}
}

// tokenTransport adds Bearer token to GitHub requests.
type tokenTransport struct {
	token string
	base  http.RoundTripper
}

func (t *tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.token != "" && isGitHubHost(req.URL.Host) {
		req = req.Clone(req.Context())
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	return t.base.RoundTrip(req)
}

// isGitHubHost checks if the host is a GitHub domain.
// Matches: api.github.com, github.com, raw.githubusercontent.com,
// objects.githubusercontent.com, etc.
func isGitHubHost(host string) bool {
	host = strings.ToLower(host)
	if host == hostGitHub || host == hostGitHubAPI {
		return true
	}
	if strings.HasSuffix(host, suffixGitHub) {
		return true
	}
	if strings.HasSuffix(host, suffixGitHubusercontent) {
		return true
	}
	return false
}
