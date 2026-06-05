package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReleaseResponse_PublishedAtDecodes(t *testing.T) {
	t.Parallel()
	// Confirms the new published_at JSON tag round-trips into time.Time
	// without a custom UnmarshalJSON.
	raw := []byte(`{"tag_name":"v1.2.3","published_at":"2024-09-13T15:42:08Z"}`)
	var r releaseResponse
	require.NoError(t, json.Unmarshal(raw, &r))
	assert.Equal(t, "v1.2.3", r.TagName)
	want := time.Date(2024, 9, 13, 15, 42, 8, 0, time.UTC)
	assert.True(t, r.PublishedAt.Equal(want), "got %v want %v", r.PublishedAt, want)
}

func TestGetLatestRelease(t *testing.T) {
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
			name:      "prefix does not match",
			tagName:   "release-1.0.0",
			tagPrefix: "v",
			want:      "release-1.0.0",
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
		{
			name:    "invalid JSON",
			body:    `{invalid`,
			wantErr: "failed to decode response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := &http.Client{
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					assert.Equal(t, "/repos/owner/repo/releases/latest", req.URL.Path)
					assert.Equal(t, "application/vnd.github+json", req.Header.Get("Accept"))

					if tt.statusCode != 0 {
						return &http.Response{
							StatusCode: tt.statusCode,
							Body:       io.NopCloser(strings.NewReader("")),
						}, nil
					}
					body := tt.body
					if body == "" {
						body = `{"tag_name":"` + tt.tagName + `"}`
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader(body)),
					}, nil
				}),
			}

			got, err := GetLatestRelease(context.Background(), client, "owner", "repo", tt.tagPrefix)
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

func TestGetLatestRelease_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		owner   string
		repo    string
		wantErr string
	}{
		{
			name:    "empty owner",
			owner:   "",
			repo:    "repo",
			wantErr: "owner and repo must not be empty",
		},
		{
			name:    "empty repo",
			owner:   "owner",
			repo:    "",
			wantErr: "owner and repo must not be empty",
		},
		{
			name:    "owner with slash",
			owner:   "ow/ner",
			repo:    "repo",
			wantErr: "must not contain '/'",
		},
		{
			name:    "repo with slash",
			owner:   "owner",
			repo:    "re/po",
			wantErr: "must not contain '/'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := GetLatestRelease(context.Background(), http.DefaultClient, tt.owner, tt.repo, "")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
