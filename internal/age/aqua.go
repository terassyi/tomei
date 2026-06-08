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
}

func (a *aquaFetcher) Fetch(ctx context.Context, key Key) (time.Time, bool, error) {
	if a.client == nil || key.Source != SourceAquaGitHubReleases {
		return time.Time{}, false, nil
	}
	if key.Owner == "" || key.Repo == "" || key.Tag == "" {
		return time.Time{}, false, nil
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
