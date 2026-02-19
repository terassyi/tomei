//go:build integration

package tests

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/tomei/internal/github"
)

// TestGetLatestRelease_HTTP tests GetLatestRelease with real HTTP communication
// via httptest.NewServer.
func TestGetLatestRelease_HTTP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		tagName    string
		tagPrefix  string
		statusCode int
		body       string
		want       string
		wantErr    string
	}{
		{
			name:    "simple tag without prefix",
			tagName: "1.25.6",
			want:    "1.25.6",
		},
		{
			name:      "tag with v prefix",
			tagName:   "v2.6.10",
			tagPrefix: "v",
			want:      "2.6.10",
		},
		{
			name:      "tag with complex prefix",
			tagName:   "bun-v1.2.3",
			tagPrefix: "bun-v",
			want:      "1.2.3",
		},
		{
			name:       "404 not found",
			statusCode: http.StatusNotFound,
			wantErr:    "GitHub API returned status 404",
		},
		{
			name:    "empty tag_name",
			body:    `{"tag_name":""}`,
			wantErr: "empty tag_name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/repos/owner/repo/releases/latest", r.URL.Path)

				if tt.statusCode != 0 {
					w.WriteHeader(tt.statusCode)
					return
				}
				body := tt.body
				if body == "" {
					body = fmt.Sprintf(`{"tag_name":%q}`, tt.tagName)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()

			// Redirect GitHub API to the test server
			client := &http.Client{
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					req.URL.Scheme = "http"
					req.URL.Host = server.Listener.Addr().String()
					return http.DefaultTransport.RoundTrip(req)
				}),
			}

			got, err := github.GetLatestRelease(context.Background(), client, "owner", "repo", tt.tagPrefix)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

