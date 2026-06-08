package age

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingFetcher records how many times Fetch was invoked; useful to
// prove the cache + singleflight dedup work.
//
// entered, when non-nil, receives a value the first time Fetch runs.
// Combined with `block`, this lets concurrency tests wait until the
// inner Fetch is provably in-flight (avoiding a flaky time.Sleep).
type countingFetcher struct {
	calls   atomic.Int64
	ts      time.Time
	ok      bool
	err     error
	block   chan struct{} // optional gate to hold goroutines in singleflight
	entered chan struct{} // optional: closed (via sync.Once) when first Fetch enters
	once    sync.Once
}

func (f *countingFetcher) Fetch(_ context.Context, _ Key) (time.Time, bool, error) {
	f.calls.Add(1)
	if f.entered != nil {
		f.once.Do(func() { close(f.entered) })
	}
	if f.block != nil {
		<-f.block
	}
	return f.ts, f.ok, f.err
}

func TestCachedFetcher_SameKeyHitsCache(t *testing.T) {
	t.Parallel()
	want := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	cf := &countingFetcher{ts: want, ok: true}
	c := &cachedFetcher{inner: cf, cache: make(map[Key]cacheEntry)}
	k := Key{Source: SourceLastModified, URL: "https://example.com/a"}

	for i := range 3 {
		got, ok, err := c.Fetch(context.Background(), k)
		if err != nil || !ok || !got.Equal(want) {
			t.Fatalf("iter %d: got (%v,%v,%v)", i, got, ok, err)
		}
	}
	if got := cf.calls.Load(); got != 1 {
		t.Errorf("inner Fetch called %d times, want 1", got)
	}
}

func TestCachedFetcher_DifferentKeysAreSeparate(t *testing.T) {
	t.Parallel()
	cf := &countingFetcher{ts: time.Now(), ok: true}
	c := &cachedFetcher{inner: cf, cache: make(map[Key]cacheEntry)}
	keys := []Key{
		{Source: SourceLastModified, URL: "https://example.com/a"},
		{Source: SourceLastModified, URL: "https://example.com/b"},
		{Source: SourceAquaGitHubReleases, Owner: "o", Repo: "r", Tag: "v1"},
	}
	for _, k := range keys {
		if _, _, err := c.Fetch(context.Background(), k); err != nil {
			t.Fatalf("Fetch %v: %v", k, err)
		}
	}
	if got := cf.calls.Load(); got != int64(len(keys)) {
		t.Errorf("inner Fetch called %d times, want %d", got, len(keys))
	}
}

func TestCachedFetcher_SingleflightDedupsConcurrent(t *testing.T) {
	t.Parallel()
	cf := &countingFetcher{
		ts:      time.Now(),
		ok:      true,
		block:   make(chan struct{}),
		entered: make(chan struct{}),
	}
	c := &cachedFetcher{inner: cf, cache: make(map[Key]cacheEntry)}
	k := Key{Source: SourceLastModified, URL: "https://example.com/x"}

	// Start gate: line up all N goroutines so they race together
	// instead of trickling in over time. Combined with the `entered`
	// channel + a brief grace period after, this gives N-1 stragglers
	// time to queue inside singleflight.Do before the first goroutine
	// completes its inner Fetch and populates the cache.
	const N = 100
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(N)
	for range N {
		go func() {
			defer wg.Done()
			<-start
			_, _, _ = c.Fetch(context.Background(), k)
		}()
	}
	close(start)

	<-cf.entered
	// Without this short grace period, stragglers could arrive at sf.Do
	// after the first goroutine returns, missing the in-flight dedup
	// and the populated cache (a narrow race observed under -race on
	// fast hardware). 20ms is comfortably above scheduler latency.
	time.Sleep(20 * time.Millisecond)
	close(cf.block)
	wg.Wait()

	if got := cf.calls.Load(); got != 1 {
		t.Errorf("inner Fetch called %d times under singleflight, want 1", got)
	}
}

// flakyFetcher returns errFirst on its first call and (ts, true, nil)
// on every call after, so a test can prove whether the first result was
// memoized (one call) or not (two calls).
type flakyFetcher struct {
	calls    atomic.Int64
	errFirst error
	ts       time.Time
}

func (f *flakyFetcher) Fetch(_ context.Context, _ Key) (time.Time, bool, error) {
	if f.calls.Add(1) == 1 {
		return time.Time{}, false, f.errFirst
	}
	return f.ts, true, nil
}

func TestCachedFetcher_ContextErrorsAreNotCached(t *testing.T) {
	t.Parallel()
	want := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	// First lookup fails with a context error; it must NOT be memoized,
	// so a later lookup (fresh ctx) re-invokes inner and can succeed.
	cf := &flakyFetcher{errFirst: context.DeadlineExceeded, ts: want}
	c := &cachedFetcher{inner: cf, cache: make(map[Key]cacheEntry)}
	k := Key{Source: SourceLastModified, URL: "https://example.com/ctx"}

	if _, _, err := c.Fetch(context.Background(), k); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first call err = %v, want DeadlineExceeded", err)
	}
	got, ok, err := c.Fetch(context.Background(), k)
	if err != nil || !ok || !got.Equal(want) {
		t.Fatalf("second call = (%v,%v,%v), want (%v,true,nil)", got, ok, err, want)
	}
	if n := cf.calls.Load(); n != 2 {
		t.Errorf("inner Fetch called %d times, want 2 (ctx error must not cache)", n)
	}
}

func TestCachedFetcher_WaiterAbortsOnContextCancel(t *testing.T) {
	t.Parallel()
	cf := &countingFetcher{
		ts:      time.Now(),
		ok:      true,
		block:   make(chan struct{}),
		entered: make(chan struct{}),
	}
	c := &cachedFetcher{inner: cf, cache: make(map[Key]cacheEntry)}
	k := Key{Source: SourceLastModified, URL: "https://example.com/y"}

	// Leader: occupies singleflight, blocked inside inner Fetch.
	leaderDone := make(chan struct{})
	go func() {
		defer close(leaderDone)
		_, _, _ = c.Fetch(context.Background(), k)
	}()
	<-cf.entered // leader is now provably in-flight

	// Waiter: same key, so it dedupes onto the leader's in-flight fetch.
	// Its ctx is canceled while waiting; it must return promptly with
	// ctx.Err() instead of blocking until the leader's fetch completes.
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, _, err := c.Fetch(ctx, k)
		errCh <- err
	}()
	// Give the waiter a moment to queue on DoChan, then cancel. The leader
	// is still blocked, so a Do-based impl would hang here until timeout.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiter err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waiter did not abort promptly on ctx cancel")
	}

	// Release the leader and let it finish cleanly.
	close(cf.block)
	<-leaderDone
}

func TestCachedFetcher_PipeInFieldDoesNotCollide(t *testing.T) {
	t.Parallel()
	// Two distinct keys whose fields would produce the same plain
	// "|"-joined string ("a|b" vs "a"+"|b"). They must NOT dedupe onto
	// one inner Fetch, i.e. keyString must keep them distinct.
	cf := &countingFetcher{ts: time.Now(), ok: true}
	c := &cachedFetcher{inner: cf, cache: make(map[Key]cacheEntry)}
	k1 := Key{Source: SourceLastModified, URL: "a|b"}
	k2 := Key{Source: SourceLastModified, Tag: "a", URL: "b"}

	if _, _, err := c.Fetch(context.Background(), k1); err != nil {
		t.Fatalf("Fetch k1: %v", err)
	}
	if _, _, err := c.Fetch(context.Background(), k2); err != nil {
		t.Fatalf("Fetch k2: %v", err)
	}
	if got := cf.calls.Load(); got != 2 {
		t.Errorf("inner Fetch called %d times, want 2 (keys must not collide)", got)
	}
}

func TestCachedFetcher_ErrorsAreCached(t *testing.T) {
	t.Parallel()
	cf := &countingFetcher{err: errors.New("transient")}
	c := &cachedFetcher{inner: cf, cache: make(map[Key]cacheEntry)}
	k := Key{Source: SourceLastModified, URL: "https://example.com/e"}

	for i := range 3 {
		_, _, err := c.Fetch(context.Background(), k)
		if err == nil {
			t.Fatalf("iter %d: expected error, got nil", i)
		}
	}
	if got := cf.calls.Load(); got != 1 {
		t.Errorf("inner Fetch called %d times, want 1 (error must cache)", got)
	}
}
