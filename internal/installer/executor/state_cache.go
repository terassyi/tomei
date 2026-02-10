package executor

import (
	"fmt"
	"sync"

	"github.com/terassyi/tomei/internal/state"
)

// StateCache holds the entire UserState in memory and flushes to disk
// when a layer completes. It provides mutex-protected access for
// cachedStore instances operating on individual maps.
type StateCache struct {
	mu    sync.Mutex
	store *state.Store[state.UserState]
	cache *state.UserState
	dirty bool
}

// NewStateCache creates a new StateCache backed by the given store.
func NewStateCache(store *state.Store[state.UserState]) *StateCache {
	return &StateCache{store: store}
}

// Init sets the in-memory cache. Call this at the start of Apply
// after loading state from disk.
func (c *StateCache) Init(st *state.UserState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = st
	c.dirty = false
}

// Flush writes the cache to disk if any changes were made.
// Call this after each layer completes.
func (c *StateCache) Flush() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.dirty {
		return nil
	}
	if err := c.store.Save(c.cache); err != nil {
		return fmt.Errorf("failed to flush state cache: %w", err)
	}
	c.dirty = false
	return nil
}

// Snapshot returns the current cache pointer.
// This is safe to call only between layers (not during parallel execution).
func (c *StateCache) Snapshot() *state.UserState {
	return c.cache
}

// withLock acquires the mutex and calls fn with the current cache.
// cachedStore uses this to access the cache without touching internal fields.
func (c *StateCache) withLock(fn func(st *state.UserState)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fn(c.cache)
}

// markDirty sets the dirty flag. Must be called while holding the mutex
// (i.e., from within a withLock callback).
func (c *StateCache) markDirty() {
	c.dirty = true
}
