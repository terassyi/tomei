package age

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/terassyi/tomei/internal/github"
)

// fakeAquaClient is a tests-only AquaReleaseClient that returns a
// preset response without touching the network.
type fakeAquaClient struct {
	release github.Release
	err     error
	calls   int
}

func (f *fakeAquaClient) GetReleaseByTag(_ context.Context, _, _, _ string) (github.Release, error) {
	f.calls++
	if f.err != nil {
		return github.Release{}, f.err
	}
	return f.release, nil
}

func TestAquaFetcher_Fetch(t *testing.T) {
	t.Parallel()
	ts := time.Date(2024, 9, 13, 15, 42, 8, 0, time.UTC)

	tests := []struct {
		name    string
		client  AquaReleaseClient
		key     Key
		wantOK  bool
		wantAt  time.Time
		wantErr bool
	}{
		{
			name:   "valid response propagates published_at",
			client: &fakeAquaClient{release: github.Release{TagName: "v1.2.3", PublishedAt: ts}},
			key:    Key{Source: SourceAquaGitHubReleases, Owner: "o", Repo: "r", Tag: "v1.2.3"},
			wantOK: true,
			wantAt: ts,
		},
		{
			name:   "nil client disables the backend (ok=false, nil)",
			client: nil,
			key:    Key{Source: SourceAquaGitHubReleases, Owner: "o", Repo: "r", Tag: "v1.2.3"},
		},
		{
			name:   "wrong source returns ok=false nil",
			client: &fakeAquaClient{},
			key:    Key{Source: SourceLastModified, URL: "https://example.com"},
		},
		{
			name:   "empty Tag returns ok=false nil",
			client: &fakeAquaClient{release: github.Release{TagName: "v1", PublishedAt: ts}},
			key:    Key{Source: SourceAquaGitHubReleases, Owner: "o", Repo: "r"},
		},
		{
			name:   "empty Owner returns ok=false nil",
			client: &fakeAquaClient{release: github.Release{TagName: "v1", PublishedAt: ts}},
			key:    Key{Source: SourceAquaGitHubReleases, Repo: "r", Tag: "v1"},
		},
		{
			name:   "empty Repo returns ok=false nil",
			client: &fakeAquaClient{release: github.Release{TagName: "v1", PublishedAt: ts}},
			key:    Key{Source: SourceAquaGitHubReleases, Owner: "o", Tag: "v1"},
		},
		{
			name:    "GetReleaseByTag error surfaces",
			client:  &fakeAquaClient{err: errors.New("404 not found")},
			key:     Key{Source: SourceAquaGitHubReleases, Owner: "o", Repo: "r", Tag: "v1"},
			wantErr: true,
		},
		{
			name:   "zero PublishedAt is treated as ok=false nil",
			client: &fakeAquaClient{release: github.Release{TagName: "v1"}}, // PublishedAt zero
			key:    Key{Source: SourceAquaGitHubReleases, Owner: "o", Repo: "r", Tag: "v1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := &aquaFetcher{client: tt.client}
			got, ok, err := f.Fetch(context.Background(), tt.key)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if ok {
					t.Errorf("expected ok=false on error, got true")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok != tt.wantOK {
				t.Errorf("ok=%v, want %v", ok, tt.wantOK)
			}
			if !got.Equal(tt.wantAt) {
				t.Errorf("publishedAt=%v, want %v", got, tt.wantAt)
			}
		})
	}
}
