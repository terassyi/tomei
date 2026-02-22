// Package resolve provides shared version resolution for runtimes and self-managed tools.
// It supports built-in resolvers ("github-release:", "http-text:") and arbitrary shell commands.
package resolve

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/terassyi/tomei/internal/github"
	"github.com/terassyi/tomei/internal/installer/command"
)

// CaptureRunner executes commands and captures stdout.
// Satisfied by *command.Executor.
type CaptureRunner interface {
	ExecuteCapture(ctx context.Context, cmds []string, vars command.Vars, env map[string]string) (string, error)
}

// Resolver resolves version strings using built-in resolvers or shell commands.
// Safe for concurrent use from multiple goroutines.
type Resolver struct {
	cmdRunner     CaptureRunner
	httpClient    *http.Client
	githubBaseURL string // override GitHub API base URL (for testing)
}

// ResolverOption configures a Resolver.
type ResolverOption func(*Resolver)

// WithGitHubBaseURL overrides the GitHub API base URL (for testing).
func WithGitHubBaseURL(url string) ResolverOption {
	return func(r *Resolver) {
		r.githubBaseURL = url
	}
}

// NewResolver creates a new Resolver.
// If httpClient is nil, a default client with GitHub token auth is used.
func NewResolver(runner CaptureRunner, httpClient *http.Client, opts ...ResolverOption) *Resolver {
	if httpClient == nil {
		httpClient = github.NewHTTPClient(github.TokenFromEnv())
	}
	r := &Resolver{
		cmdRunner:  runner,
		httpClient: httpClient,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Resolve resolves a version using built-in resolvers or shell commands.
// Supported formats:
//   - "github-release:owner/repo:tagPrefix" — GitHub API latest release
//   - "http-text:URL:regex" — HTTP text fetch + regex match
//   - arbitrary shell command — ExecuteCapture fallback
func (r *Resolver) Resolve(ctx context.Context, cmds []string, vars command.Vars) (string, error) {
	if len(cmds) == 0 {
		return "", fmt.Errorf("resolve: empty commands")
	}

	cmd := cmds[0]

	// Built-in GitHub release resolver
	if strings.HasPrefix(cmd, "github-release:") {
		return r.resolveGitHubRelease(ctx, cmd)
	}

	// Built-in HTTP text resolver
	if strings.HasPrefix(cmd, "http-text:") {
		return r.resolveHTTPText(ctx, cmd)
	}

	// Shell command fallback
	slog.Debug("resolving version via command", "command", cmd)
	version, err := r.cmdRunner.ExecuteCapture(ctx, cmds, vars, nil)
	if err != nil {
		return "", err
	}
	if version == "" {
		return "", fmt.Errorf("resolveVersion command returned empty result")
	}

	slog.Debug("version resolved via command", "resolved", version)
	return version, nil
}

// resolveGitHubRelease parses "github-release:owner/repo:tagPrefix" and fetches the latest release.
func (r *Resolver) resolveGitHubRelease(ctx context.Context, cmd string) (string, error) {
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

	slog.Debug("resolving version via GitHub release", "owner", owner, "repo", repo, "tagPrefix", tagPrefix)

	var version string
	var err error
	if r.githubBaseURL != "" {
		version, err = github.GetLatestReleaseWithBase(ctx, r.httpClient, owner, repo, tagPrefix, r.githubBaseURL)
	} else {
		version, err = github.GetLatestRelease(ctx, r.httpClient, owner, repo, tagPrefix)
	}
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
func (r *Resolver) resolveHTTPText(ctx context.Context, cmd string) (string, error) {
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := r.httpClient.Do(req)
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
				slog.Debug("version resolved via http-text", "version", m[1])
				return m[1], nil
			}
			slog.Debug("version resolved via http-text", "version", m[0])
			return m[0], nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to scan response body: %w", err)
	}

	return "", fmt.Errorf("http-text: no match for regex %q in response from %s", pattern, url)
}
