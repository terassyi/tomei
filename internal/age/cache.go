package age

import (
	"context"
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
// consistency, and the package does not retry.
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
		c.mu.Lock()
		c.cache[key] = cacheEntry{publishedAt: t, ok: ok, err: e}
		c.mu.Unlock()
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
// human-readable, only unique per Key value.
func keyString(k Key) string {
	return fmt.Sprintf("%s|%s|%s|%s|%s", k.Source, k.Owner, k.Repo, k.Tag, k.URL)
}
