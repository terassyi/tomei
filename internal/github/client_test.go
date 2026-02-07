package github

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenFromEnv(t *testing.T) {
	tests := []struct {
		name        string
		githubToken string
		ghToken     string
		want        string
	}{
		{
			name: "neither set",
			want: "",
		},
		{
			name:        "GITHUB_TOKEN set",
			githubToken: "ghp_github",
			want:        "ghp_github",
		},
		{
			name:    "GH_TOKEN set",
			ghToken: "ghp_gh",
			want:    "ghp_gh",
		},
		{
			name:        "both set, GITHUB_TOKEN takes precedence",
			githubToken: "ghp_github",
			ghToken:     "ghp_gh",
			want:        "ghp_github",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(envGitHubToken, tt.githubToken)
			t.Setenv(envGHToken, tt.ghToken)

			got := TokenFromEnv()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsGitHubHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"github.com", true},
		{"api.github.com", true},
		{"raw.githubusercontent.com", true},
		{"objects.githubusercontent.com", true},
		{"uploads.github.com", true},
		{"example.com", false},
		{"notgithub.com", false},
		{"githubusercontent.com", false},
		{"evil.github.com.example.com", false},
		{"GITHUB.COM", true},
		{"API.GITHUB.COM", true},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			assert.Equal(t, tt.want, isGitHubHost(tt.host))
		})
	}
}

func TestTokenTransport(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		url      string
		wantAuth string
	}{
		{
			name:     "adds auth header to GitHub API",
			token:    "test-token",
			url:      "https://api.github.com/repos/foo/bar",
			wantAuth: "Bearer test-token",
		},
		{
			name:     "adds auth header to GitHub releases",
			token:    "test-token",
			url:      "https://github.com/foo/bar/releases/download/v1.0/asset.tar.gz",
			wantAuth: "Bearer test-token",
		},
		{
			name:     "adds auth header to raw.githubusercontent.com",
			token:    "test-token",
			url:      "https://raw.githubusercontent.com/foo/bar/main/file",
			wantAuth: "Bearer test-token",
		},
		{
			name:     "does not add auth header to non-GitHub host",
			token:    "test-token",
			url:      "https://example.com/file.tar.gz",
			wantAuth: "",
		},
		{
			name:     "no auth header when token is empty",
			token:    "",
			url:      "https://api.github.com/repos/foo/bar",
			wantAuth: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotAuth string
			transport := &tokenTransport{
				token: tt.token,
				base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					gotAuth = req.Header.Get("Authorization")
					return &http.Response{StatusCode: 200}, nil
				}),
			}

			req, err := http.NewRequest("GET", tt.url, nil)
			require.NoError(t, err)

			_, err = transport.RoundTrip(req)
			require.NoError(t, err)
			assert.Equal(t, tt.wantAuth, gotAuth)
		})
	}
}

func TestNewHTTPClient(t *testing.T) {
	t.Run("with token", func(t *testing.T) {
		client := NewHTTPClient("my-token")
		assert.NotNil(t, client)
		assert.Equal(t, defaultTimeout, client.Timeout)

		transport, ok := client.Transport.(*tokenTransport)
		require.True(t, ok)
		assert.Equal(t, "my-token", transport.token)
	})

	t.Run("with empty token", func(t *testing.T) {
		client := NewHTTPClient("")
		assert.NotNil(t, client)

		transport, ok := client.Transport.(*tokenTransport)
		require.True(t, ok)
		assert.Empty(t, transport.token)
	})
}

func TestNewHTTPClient_EndToEnd(t *testing.T) {
	tests := []struct {
		name        string
		githubToken string
		ghToken     string
		wantAuth    string
	}{
		{
			name:        "GITHUB_TOKEN flows to Authorization header",
			githubToken: "ghp_test_github_token",
			wantAuth:    "Bearer ghp_test_github_token",
		},
		{
			name:     "GH_TOKEN flows to Authorization header",
			ghToken:  "ghp_test_gh_token",
			wantAuth: "Bearer ghp_test_gh_token",
		},
		{
			name:        "GITHUB_TOKEN takes precedence over GH_TOKEN",
			githubToken: "ghp_primary",
			ghToken:     "ghp_secondary",
			wantAuth:    "Bearer ghp_primary",
		},
		{
			name:     "no token means no Authorization header",
			wantAuth: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(envGitHubToken, tt.githubToken)
			t.Setenv(envGHToken, tt.ghToken)

			// Full flow: env → TokenFromEnv → NewHTTPClient → HTTP request
			token := TokenFromEnv()
			client := NewHTTPClient(token)

			var gotAuth string
			// Replace the base transport to capture the request
			transport := client.Transport.(*tokenTransport)
			transport.base = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				gotAuth = req.Header.Get("Authorization")
				return &http.Response{StatusCode: 200}, nil
			})

			req, err := http.NewRequest("GET", "https://api.github.com/repos/owner/repo/releases/latest", nil)
			require.NoError(t, err)

			_, err = client.Do(req)
			require.NoError(t, err)
			assert.Equal(t, tt.wantAuth, gotAuth)
		})
	}
}

// roundTripFunc is a helper for mocking http.RoundTripper in tests.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
