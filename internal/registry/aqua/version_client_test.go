package aqua

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionClient_GetLatestRef(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       string
		wantErr    string
	}{
		{
			name:       "success",
			statusCode: http.StatusOK,
			body:       `{"tag_name": "v4.500.0"}`,
			want:       "v4.500.0",
		},
		{
			name:       "empty tag_name",
			statusCode: http.StatusOK,
			body:       `{"tag_name": ""}`,
			wantErr:    "tag_name is empty",
		},
		{
			name:       "http error",
			statusCode: http.StatusInternalServerError,
			body:       "",
			wantErr:    "unexpected status code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mockClient := &http.Client{
				Transport: &mockRoundTripper{
					handler: func(req *http.Request) (*http.Response, error) {
						assert.Equal(t, "application/vnd.github.v3+json", req.Header.Get("Accept"))
						assert.Contains(t, req.URL.String(), "api.github.com")
						return newMockResponse(tt.statusCode, tt.body), nil
					},
				},
			}

			client := NewVersionClient(mockClient)
			got, err := client.GetLatestRef(context.Background())

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

func TestVersionClient_GetLatestToolVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		repoOwner  string
		repoName   string
		statusCode int
		body       string
		want       string
		wantErr    string
	}{
		{
			name:       "success",
			repoOwner:  "cli",
			repoName:   "cli",
			statusCode: http.StatusOK,
			body:       `{"tag_name": "v2.86.0"}`,
			want:       "v2.86.0",
		},
		{
			name:      "invalid repo owner",
			repoOwner: "../etc",
			repoName:  "cli",
			wantErr:   "invalid repo owner",
		},
		{
			name:      "invalid repo name",
			repoOwner: "cli",
			repoName:  "../etc",
			wantErr:   "invalid repo name",
		},
		{
			name:       "empty tag_name",
			repoOwner:  "cli",
			repoName:   "cli",
			statusCode: http.StatusOK,
			body:       `{"tag_name": ""}`,
			wantErr:    "tag_name is empty",
		},
		{
			name:       "http error",
			repoOwner:  "nonexistent",
			repoName:   "repo",
			statusCode: http.StatusNotFound,
			body:       "",
			wantErr:    "unexpected status code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mockClient := &http.Client{
				Transport: &mockRoundTripper{
					handler: func(req *http.Request) (*http.Response, error) {
						return newMockResponse(tt.statusCode, tt.body), nil
					},
				},
			}

			client := NewVersionClient(mockClient)
			got, err := client.GetLatestToolVersion(context.Background(), tt.repoOwner, tt.repoName)

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

func TestVersionClient_GetReleaseByTag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		owner       string
		repo        string
		tag         string
		statusCode  int
		body        string
		wantTag     string
		wantPubAt   time.Time
		wantPubZero bool
		wantErr     string
	}{
		{
			name:       "success returns tag and publishedAt",
			owner:      "BurntSushi",
			repo:       "ripgrep",
			tag:        "14.1.0",
			statusCode: http.StatusOK,
			body:       `{"tag_name":"14.1.0","published_at":"2024-09-13T15:42:08Z"}`,
			wantTag:    "14.1.0",
			wantPubAt:  time.Date(2024, 9, 13, 15, 42, 8, 0, time.UTC),
		},
		{
			name:        "missing published_at decodes to zero time",
			owner:       "o",
			repo:        "r",
			tag:         "v1",
			statusCode:  http.StatusOK,
			body:        `{"tag_name":"v1"}`,
			wantTag:     "v1",
			wantPubZero: true,
		},
		{
			name:    "invalid owner path component",
			owner:   "../etc",
			repo:    "r",
			tag:     "v1",
			wantErr: "invalid repo owner",
		},
		{
			name:    "invalid repo path component",
			owner:   "o",
			repo:    "../etc",
			tag:     "v1",
			wantErr: "invalid repo name",
		},
		{
			name:    "invalid tag path component",
			owner:   "o",
			repo:    "r",
			tag:     "../etc",
			wantErr: "invalid tag",
		},
		{
			name:       "non-2xx returns error",
			owner:      "o",
			repo:       "r",
			tag:        "v1",
			statusCode: http.StatusNotFound,
			body:       "",
			wantErr:    "unexpected status code",
		},
		{
			name:       "empty tag_name returns error",
			owner:      "o",
			repo:       "r",
			tag:        "v1",
			statusCode: http.StatusOK,
			body:       `{"tag_name":""}`,
			wantErr:    "tag_name is empty",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mockClient := &http.Client{
				Transport: &mockRoundTripper{
					handler: func(req *http.Request) (*http.Response, error) {
						assert.Equal(t, "application/vnd.github.v3+json", req.Header.Get("Accept"))
						assert.Contains(t, req.URL.String(), "/releases/tags/")
						return newMockResponse(tt.statusCode, tt.body), nil
					},
				},
			}
			client := NewVersionClient(mockClient)
			got, err := client.GetReleaseByTag(context.Background(), tt.owner, tt.repo, tt.tag)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantTag, got.TagName)
			if tt.wantPubZero {
				assert.True(t, got.PublishedAt.IsZero(), "expected zero PublishedAt, got %v", got.PublishedAt)
				return
			}
			assert.True(t, got.PublishedAt.Equal(tt.wantPubAt),
				"PublishedAt = %v, want %v", got.PublishedAt, tt.wantPubAt)
		})
	}
}
