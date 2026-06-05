package age

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// recordingFetcher remembers which Source it last saw, for dispatch tests.
type recordingFetcher struct {
	id      string // backend identifier, e.g. "aqua" or "lastmodified"
	called  atomic.Pointer[string]
	wantTS  time.Time
	wantOK  bool
	wantErr error
}

func (r *recordingFetcher) Fetch(_ context.Context, _ Key) (time.Time, bool, error) {
	v := r.id
	r.called.Store(&v)
	return r.wantTS, r.wantOK, r.wantErr
}

func TestDispatchFetcher_RoutesBySource(t *testing.T) {
	t.Parallel()
	ts := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	aq := &recordingFetcher{id: "aqua", wantTS: ts, wantOK: true}
	lm := &recordingFetcher{id: "lastmodified", wantTS: ts, wantOK: true}
	d := &dispatchFetcher{aqua: aq, lastModified: lm}

	if _, _, err := d.Fetch(context.Background(), Key{Source: SourceAquaGitHubReleases, Owner: "o", Repo: "r", Tag: "v1"}); err != nil {
		t.Fatalf("aqua dispatch: %v", err)
	}
	if got := aq.called.Load(); got == nil || *got != "aqua" {
		t.Errorf("aqua backend not invoked")
	}
	if got := lm.called.Load(); got != nil {
		t.Errorf("lastmodified backend invoked for aqua key")
	}

	lm.called.Store(nil)
	aq.called.Store(nil)
	if _, _, err := d.Fetch(context.Background(), Key{Source: SourceLastModified, URL: "https://example.com"}); err != nil {
		t.Fatalf("lastmodified dispatch: %v", err)
	}
	if got := lm.called.Load(); got == nil || *got != "lastmodified" {
		t.Errorf("lastmodified backend not invoked")
	}
	if got := aq.called.Load(); got != nil {
		t.Errorf("aqua backend invoked for lastmodified key")
	}
}

func TestDispatchFetcher_UnknownSourceReturnsOKFalse(t *testing.T) {
	t.Parallel()
	d := &dispatchFetcher{
		aqua:         &recordingFetcher{id: "aqua"},
		lastModified: &recordingFetcher{id: "lastmodified"},
	}
	_, ok, err := d.Fetch(context.Background(), Key{Source: "weird"})
	if ok || err != nil {
		t.Errorf("unknown source should be (ok=false, nil); got ok=%v err=%v", ok, err)
	}
}

func TestNew_AllNilStillBuildsFetcher(t *testing.T) {
	t.Parallel()
	f := New(nil, nil)
	if f == nil {
		t.Fatal("New(nil, nil) returned nil Fetcher")
	}
	// Aqua key → ok=false (aquaFetcher is built with nil client).
	if _, ok, err := f.Fetch(context.Background(), Key{Source: SourceAquaGitHubReleases, Owner: "o", Repo: "r", Tag: "v1"}); ok || err != nil {
		t.Errorf("aqua with nil client: got (ok=%v, err=%v), want (false, nil)", ok, err)
	}
	// LastModified key with empty URL → ok=false (early return before any
	// network or SSRF gate runs). Confirms the default httpClient
	// constructed inside New is wired into the lastModified backend.
	if _, ok, err := f.Fetch(context.Background(), Key{Source: SourceLastModified, URL: ""}); ok || err != nil {
		t.Errorf("lastModified with empty URL: got (ok=%v, err=%v), want (false, nil)", ok, err)
	}
}

func TestFetchAll_EmptyKeys(t *testing.T) {
	t.Parallel()
	results := FetchAll(context.Background(), &fixedFetcher{ts: time.Now()}, nil)
	if len(results) != 0 {
		t.Errorf("len(results)=%d, want 0 for nil keys", len(results))
	}
	results = FetchAll(context.Background(), &fixedFetcher{ts: time.Now()}, []Key{})
	if len(results) != 0 {
		t.Errorf("len(results)=%d, want 0 for empty keys slice", len(results))
	}
}

func TestFetchAll_HonorsOptionsFromNew(t *testing.T) {
	t.Parallel()
	// Options passed to New propagate to FetchAll via the cachedFetcher's
	// stored config, so callers don't have to re-pass them at every
	// FetchAll. A very small WithBatchTimeout makes the assertion
	// trivially observable: a blocking fetcher exits via ctx.Err in
	// well under the 60s default.
	f := New(nil, nil, WithBatchTimeout(100*time.Millisecond))
	// Replace the inner with a blocking fetcher so the timeout actually
	// fires; this requires direct field access (same package).
	cf := f.(*cachedFetcher)
	cf.inner = &blockingFetcher{dur: 10 * time.Second}

	start := time.Now()
	results := FetchAll(context.Background(), f, []Key{
		{Source: SourceLastModified, URL: numberedURL(0)},
	})
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Errorf("FetchAll took %v — WithBatchTimeout from New was not honored", elapsed)
	}
	if len(results) != 1 || results[0].Err == nil {
		t.Errorf("expected one result with ctx error, got %+v", results)
	}
}

func TestFetchAll_PreCanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	keys := []Key{{Source: SourceLastModified, URL: numberedURL(0)}}
	results := FetchAll(ctx, &fixedFetcher{ts: time.Now()}, keys)
	if len(results) != 1 {
		t.Fatalf("len(results)=%d, want 1", len(results))
	}
	if results[0].Err == nil {
		t.Errorf("expected ctx error, got nil")
	}
}

// fixedFetcher returns the same triple for every key. Used as the inner
// Fetcher in FetchAll tests where we just want the batch mechanics
// exercised, not per-Source dispatch.
type fixedFetcher struct {
	ts time.Time
}

func (f *fixedFetcher) Fetch(_ context.Context, _ Key) (time.Time, bool, error) {
	return f.ts, true, nil
}

func TestFetchAll_PreservesOrder(t *testing.T) {
	t.Parallel()
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	keys := make([]Key, 20)
	for i := range keys {
		keys[i] = Key{Source: SourceLastModified, URL: numberedURL(i)}
	}
	results := FetchAll(context.Background(), &fixedFetcher{ts: ts}, keys, WithConcurrency(5))
	if len(results) != len(keys) {
		t.Fatalf("len(results)=%d, want %d", len(results), len(keys))
	}
	for i := range keys {
		if results[i].Key != keys[i] {
			t.Errorf("results[%d].Key=%v, want %v", i, results[i].Key, keys[i])
		}
		if !results[i].PublishedAt.Equal(ts) || !results[i].OK || results[i].Err != nil {
			t.Errorf("results[%d] = %+v", i, results[i])
		}
	}
}

func numberedURL(i int) string {
	return "https://example.com/k" + strconv.Itoa(i)
}

// blockingFetcher sleeps for `dur` to simulate slow upstream; lets us
// prove FetchAll's batch timeout actually cancels in-flight goroutines.
type blockingFetcher struct {
	dur time.Duration
}

func (b *blockingFetcher) Fetch(ctx context.Context, _ Key) (time.Time, bool, error) {
	select {
	case <-time.After(b.dur):
		return time.Time{}, false, nil
	case <-ctx.Done():
		return time.Time{}, false, ctx.Err()
	}
}

func TestFetchAll_BatchTimeoutCancelsInFlight(t *testing.T) {
	t.Parallel()
	keys := make([]Key, 5)
	for i := range keys {
		keys[i] = Key{Source: SourceLastModified, URL: numberedURL(i)}
	}
	start := time.Now()
	results := FetchAll(context.Background(), &blockingFetcher{dur: 10 * time.Second}, keys,
		WithConcurrency(2),
		WithBatchTimeout(100*time.Millisecond))
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Errorf("batch took %v, expected ~100ms — timeout did not cancel in-flight", elapsed)
	}
	for i, r := range results {
		if r.Err == nil {
			t.Errorf("results[%d].Err == nil, expected ctx error after timeout", i)
		}
	}
}
