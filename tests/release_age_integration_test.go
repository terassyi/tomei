//go:build integration

package tests

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/tomei/internal/age"
	"github.com/terassyi/tomei/internal/github"
	"github.com/terassyi/tomei/internal/registry/aqua"
)

// TestReleaseAgeFetcher_Aqua_RealGitHub exercises the minimumReleaseAge gate's
// aqua backend end-to-end against the real GitHub Releases API: the path the
// engine takes is age.New(aqua.NewVersionClient(...)) → GetReleaseByTag →
// published_at decode.
//
// It asserts only the time-monotonic direction to avoid rot: a long-ago release
// resolves a real publication time before a hard-coded past date. (Gate-hit /
// "younger than threshold" decisions are covered deterministically by the unit
// fake in internal/installer/engine; a live "recent release" assertion would
// invert as the release ages.)
func TestReleaseAgeFetcher_Aqua_RealGitHub(t *testing.T) {
	token := github.TokenFromEnv()
	if token == "" {
		t.Skip("real-GitHub release-age test requires GITHUB_TOKEN or GH_TOKEN (unauthenticated rate limit is 60/h)")
	}

	fetcher := age.New(aqua.NewVersionClient(github.NewHTTPClient(token)), nil)

	// cli/cli v1.0.0 was published 2020-09-16; this fact never changes.
	key := age.Key{Source: age.SourceAquaGitHubReleases, Owner: "cli", Repo: "cli", Tag: "v1.0.0"}
	publishedAt, ok, err := fetcher.Fetch(context.Background(), key)
	require.NoError(t, err)
	require.True(t, ok, "a known release must resolve a publication time")

	cutoff := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	assert.True(t, publishedAt.Before(cutoff),
		"cli/cli v1.0.0 should resolve a 2020 publish time, got %s", publishedAt)
	// And it is comfortably older than any realistic threshold → would install.
	assert.Greater(t, time.Since(publishedAt), 8760*time.Hour, "release should be >1y old")
}

// TestReleaseAgeFetcher_Aqua_TagMismatch_FailsOpen documents the v1 limitation:
// when the GitHub tag cannot be found (e.g. version_prefix mismatch), the
// fetcher reports the failure rather than a timestamp, and the engine gate
// fails open (surfaced via UnverifiedReleaseAge).
func TestReleaseAgeFetcher_Aqua_TagMismatch_FailsOpen(t *testing.T) {
	token := github.TokenFromEnv()
	if token == "" {
		t.Skip("real-GitHub release-age test requires GITHUB_TOKEN or GH_TOKEN")
	}

	fetcher := age.New(aqua.NewVersionClient(github.NewHTTPClient(token)), nil)
	key := age.Key{Source: age.SourceAquaGitHubReleases, Owner: "cli", Repo: "cli", Tag: "v0.0.0-does-not-exist"}
	_, ok, err := fetcher.Fetch(context.Background(), key)
	assert.False(t, ok, "a nonexistent tag must not resolve a timestamp")
	assert.Error(t, err, "a 404 surfaces as an error → gate fails open")
}
