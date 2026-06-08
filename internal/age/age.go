// Package age resolves release publication times for tomei tools.
//
// The minimumReleaseAge supply-chain defense (issue #257) needs to know
// when each tool's selected version was published upstream. This package
// abstracts that lookup behind a Fetcher interface with two backends:
//
//   - SourceAquaGitHubReleases — GitHub Releases API's published_at,
//     consulted for tools installed via the builtin aqua installer.
//   - SourceLastModified — HTTP Last-Modified header (with a HEAD→GET
//     Range fallback when HEAD returns 405/501, or succeeds but omits
//     Last-Modified), consulted for tools installed via the builtin
//     download installer.
//
// Tools installed via delegation (custom Installer or Runtime commands)
// are out of scope here — issue #253 threads the duration string through
// to the user's install commands and the user's command is responsible
// for whatever check it wants.
package age

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/terassyi/tomei/internal/github"
	"golang.org/x/sync/semaphore"
)

// Source identifies which backend resolves a Key to a publication time.
type Source string

const (
	// SourceAquaGitHubReleases looks up published_at via the GitHub
	// Releases API. Key fields: Owner, Repo, Tag.
	SourceAquaGitHubReleases Source = "aqua-github-release"
	// SourceLastModified looks up the upstream HTTP Last-Modified header
	// for a raw download URL. Key field: URL.
	SourceLastModified Source = "last-modified"
)

// Key identifies a single artifact whose publication time the caller
// wants to look up. Fields are interpreted per Source — fields not
// consumed by a given Source are ignored. Key is a comparable value
// (suitable as a map key) so the cache can index by it directly.
type Key struct {
	Source           Source
	Owner, Repo, Tag string // SourceAquaGitHubReleases
	URL              string // SourceLastModified
}

// Fetcher resolves a Key to its upstream publication time.
//
// Return semantics:
//
//	ok=true, err=nil   → publishedAt is a real upstream timestamp
//	ok=false, err=nil  → this source cannot supply a timestamp for this
//	                     key (missing required field, server has no
//	                     Last-Modified header, aquaClient is nil, etc).
//	                     Callers MUST treat this as "gate disabled for
//	                     this key", not an error — a single missing
//	                     header must not block apply.
//	ok=false, err!=nil → network / parse / HTTP-non-2xx failure. Callers
//	                     decide whether to fail-closed (block install)
//	                     or fail-open. The package does not retry.
type Fetcher interface {
	Fetch(ctx context.Context, key Key) (publishedAt time.Time, ok bool, err error)
}

// AquaReleaseClient is the subset of internal/registry/aqua.VersionClient
// that the aqua backend needs. Declared as an interface so tests can
// inject fakes without constructing a full VersionClient + HTTP client.
type AquaReleaseClient interface {
	GetReleaseByTag(ctx context.Context, owner, repo, tag string) (github.Release, error)
}

// Result is one element of a FetchAll batch result.
type Result struct {
	Key         Key
	PublishedAt time.Time
	OK          bool
	Err         error
}

// Option configures the package's defaults via functional options.
type Option func(*config)

type config struct {
	perRequestTimeout time.Duration
	concurrency       int
	batchTimeout      time.Duration
	allowLoopback     bool // test-only escape; not exposed publicly
}

func defaultConfig() *config {
	return &config{
		perRequestTimeout: 10 * time.Second,
		concurrency:       5,
		batchTimeout:      60 * time.Second,
	}
}

// WithPerRequestTimeout caps each individual HTTP fetch. The package
// applies it via context.WithTimeout — the http.Client itself has no
// Client.Timeout so the body read for Range-GET fallback is not cut
// short separately. Non-positive values are ignored (default 10s).
func WithPerRequestTimeout(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.perRequestTimeout = d
		}
	}
}

// WithConcurrency caps parallel fetches in FetchAll (default 5).
// Non-positive values are ignored.
func WithConcurrency(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.concurrency = n
		}
	}
}

// WithBatchTimeout sets the outer context.WithTimeout that FetchAll
// applies to the whole batch (default 60s). Non-positive values are
// ignored.
func WithBatchTimeout(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.batchTimeout = d
		}
	}
}

// New returns a cached Fetcher that dispatches by Source.
//
// httpClient is used for SourceLastModified; pass nil to get a default
// client with the package's SSRF-hardened CheckRedirect installed (HTTPS
// only, no private/loopback/link-local/multicast/unspecified IPs, at
// most 5 redirect hops). When a non-nil httpClient is supplied the
// caller is responsible for installing equivalent guards.
//
// aquaClient is used for SourceAquaGitHubReleases; nil means the aqua
// backend is disabled (its keys return ok=false, nil err).
//
// GitHub API rate limit: unauthenticated requests are capped at 60/h;
// authenticated at 5000/h. For manifests with many aqua-installed tools,
// set GITHUB_TOKEN (or GH_TOKEN) and build the aquaClient with
// github.NewHTTPClient(github.TokenFromEnv()).
//
// Cache lifetime is the Fetcher instance. Callers should construct one
// Fetcher per tomei plan/apply invocation and discard afterwards.
// Errors are cached for invocation consistency (transient failures are
// NOT retried within a single invocation).
func New(aquaClient AquaReleaseClient, httpClient *http.Client, opts ...Option) Fetcher {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}
	if httpClient == nil {
		// No client-level Timeout (context controls deadline);
		// CheckRedirect re-validates every hop and caps at 5.
		httpClient = &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("stopped after 5 redirects")
				}
				return validateRedirectURL(req.URL, cfg.allowLoopback)
			},
		}
	}
	inner := &dispatchFetcher{
		aqua:         &aquaFetcher{client: aquaClient},
		lastModified: &lastModifiedFetcher{client: httpClient, timeout: cfg.perRequestTimeout, allowLoopback: cfg.allowLoopback},
	}
	return &cachedFetcher{inner: inner, cache: make(map[Key]cacheEntry), cfg: cfg}
}

// configurableFetcher is implemented by the Fetcher returned from New.
// FetchAll uses it to honor options the caller set on the Fetcher
// without requiring them to be re-passed. Custom Fetcher implementations
// supplied to FetchAll fall back to defaults.
type configurableFetcher interface {
	Fetcher
	config() *config
}

// batchConfigFrom returns a fresh config seeded from the Fetcher's own
// configuration (if it carries one), so FetchAll honors options set at
// New() time. The returned config is a copy — caller-supplied opts
// override it without mutating the Fetcher.
func batchConfigFrom(f Fetcher) *config {
	if cf, ok := f.(configurableFetcher); ok {
		c := *cf.config()
		return &c
	}
	return defaultConfig()
}

// dispatchFetcher routes Fetch calls to the right backend by Source.
type dispatchFetcher struct {
	aqua         Fetcher
	lastModified Fetcher
}

func (d *dispatchFetcher) Fetch(ctx context.Context, key Key) (time.Time, bool, error) {
	switch key.Source {
	case SourceAquaGitHubReleases:
		return d.aqua.Fetch(ctx, key)
	case SourceLastModified:
		return d.lastModified.Fetch(ctx, key)
	default:
		return time.Time{}, false, nil
	}
}

// FetchAll resolves every key in parallel, bounded by WithConcurrency
// (default 5). The whole batch is wrapped in
// context.WithTimeout(ctx, WithBatchTimeout) (default 60s). Results are
// returned in the same order as keys; keys that couldn't acquire a
// semaphore slot before ctx expired carry Err set to the ctx error.
//
// Options precedence: starts from the config the Fetcher was built with
// (so options passed to New are honored without re-passing), then any
// opts supplied here override per-call. Custom Fetcher implementations
// that don't satisfy configurableFetcher start from package defaults.
func FetchAll(ctx context.Context, f Fetcher, keys []Key, opts ...Option) []Result {
	cfg := batchConfigFrom(f)
	for _, opt := range opts {
		opt(cfg)
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.batchTimeout)
	defer cancel()

	results := make([]Result, len(keys))
	sem := semaphore.NewWeighted(int64(cfg.concurrency))
	var wg sync.WaitGroup
	for i, k := range keys {
		if err := sem.Acquire(ctx, 1); err != nil {
			results[i] = Result{Key: k, Err: err}
			continue
		}
		wg.Add(1)
		go func(i int, k Key) {
			defer wg.Done()
			defer sem.Release(1)
			t, ok, err := f.Fetch(ctx, k)
			results[i] = Result{Key: k, PublishedAt: t, OK: ok, Err: err}
		}(i, k)
	}
	wg.Wait()
	return results
}
