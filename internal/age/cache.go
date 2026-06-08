package age

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// cachedFetcher wraps another Fetcher with an in-memory result cache
// (keyed by Key) plus a singleflight.Group so concurrent calls for the
// same key dedupe to one inner Fetch. Cache lifetime is the wrapper's
// lifetime — one tomei plan/apply invocation creates one cachedFetcher
// and discards it afterwards.
//
// Errors are cached: a transient network failure on first lookup is
// returned for every subsequent call to that key in the same
// invocation. This is intentional — callers expect invocation-local
// consistency, and the package does not retry. The one exception is
// context cancellation/timeout: those are caller-driven and per-call,
// not upstream-derived, so they are not memoized (otherwise one batch
// timeout would poison the key for every later lookup, even under a
// fresh context).
type cachedFetcher struct {
	inner Fetcher
	mu    sync.Mutex
	cache map[Key]cacheEntry
	sf    singleflight.Group
	cfg   *config // captured from New; surfaced via config() for FetchAll
}

// config returns the configuration captured at New() time. FetchAll
// uses this to honor options the caller set on the Fetcher (notably
// WithConcurrency and WithBatchTimeout) without requiring them to be
// re-passed.
func (c *cachedFetcher) config() *config { return c.cfg }

type cacheEntry struct {
	publishedAt time.Time
	ok          bool
	err         error
}

func (c *cachedFetcher) Fetch(ctx context.Context, key Key) (time.Time, bool, error) {
	c.mu.Lock()
	if e, found := c.cache[key]; found {
		c.mu.Unlock()
		return e.publishedAt, e.ok, e.err
	}
	c.mu.Unlock()

	// Concurrent calls for the same key dedupe to one inner Fetch.
	type sfResult struct {
		publishedAt time.Time
		ok          bool
	}
	// DoChan (not Do) so a caller whose ctx is canceled while waiting on
	// another goroutine's in-flight fetch can abort promptly via the
	// select below, instead of blocking until that shared fetch returns.
	ch := c.sf.DoChan(keyString(key), func() (any, error) {
		t, ok, e := c.inner.Fetch(ctx, key)
		// Don't memoize context cancellation/timeout — those are
		// caller-driven and per-call, so caching them would poison the
		// key for the rest of the invocation. Only upstream-derived
		// results/errors are cached.
		if !errors.Is(e, context.Canceled) && !errors.Is(e, context.DeadlineExceeded) {
			c.mu.Lock()
			c.cache[key] = cacheEntry{publishedAt: t, ok: ok, err: e}
			c.mu.Unlock()
		}
		return sfResult{publishedAt: t, ok: ok}, e
	})
	select {
	case <-ctx.Done():
		return time.Time{}, false, ctx.Err()
	case res := <-ch:
		r := res.Val.(sfResult)
		return r.publishedAt, r.ok, res.Err
	}
}

// keyString builds the singleflight dedup key. Doesn't have to be
// human-readable, only unique per Key value. Fields are %q-quoted (not
// plain "|"-joined) so a "|" embedded in any field — git tags and URLs
// can contain one — can't make two distinct Keys collide onto the same
// dedup string.
func keyString(k Key) string {
	return fmt.Sprintf("%q|%q|%q|%q|%q", k.Source, k.Owner, k.Repo, k.Tag, k.URL)
}
