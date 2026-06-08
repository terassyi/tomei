package age

import (
	"context"
	"time"
)

// aquaFetcher resolves SourceAquaGitHubReleases keys via the GitHub
// Releases API published_at field. A nil client disables the backend
// (all aqua keys return ok=false, nil).
type aquaFetcher struct {
	client AquaReleaseClient
	// timeout bounds a single GetReleaseByTag call. Zero means no per-request
	// bound is applied here (the client's own Timeout, if any, still applies).
	timeout time.Duration
}

func (a *aquaFetcher) Fetch(ctx context.Context, key Key) (time.Time, bool, error) {
	if a.client == nil || key.Source != SourceAquaGitHubReleases {
		return time.Time{}, false, nil
	}
	if key.Owner == "" || key.Repo == "" || key.Tag == "" {
		return time.Time{}, false, nil
	}
	if a.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, a.timeout)
		defer cancel()
	}
	rel, err := a.client.GetReleaseByTag(ctx, key.Owner, key.Repo, key.Tag)
	if err != nil {
		return time.Time{}, false, err
	}
	if rel.PublishedAt.IsZero() {
		return time.Time{}, false, nil
	}
	return rel.PublishedAt, true, nil
}
