package executor

import (
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/state"
)

// cachedStore implements StateStore[S] by operating on a single map
// within the StateCache via a mapAccessor.
type cachedStore[S resource.State] struct {
	cache    *StateCache
	accessor mapAccessor[S]
}

// Load retrieves a state entry by name from the cache.
func (s *cachedStore[S]) Load(name string) (S, bool, error) {
	var result S
	var exists bool
	s.cache.withLock(func(st *state.UserState) {
		result, exists = s.accessor.get(st, name)
	})
	return result, exists, nil
}

// Save stores a state entry in the cache and marks it dirty.
func (s *cachedStore[S]) Save(name string, val S) error {
	s.cache.withLock(func(st *state.UserState) {
		s.accessor.set(st, name, val)
		s.cache.markDirty()
	})
	return nil
}

// Delete removes a state entry from the cache and marks it dirty.
func (s *cachedStore[S]) Delete(name string) error {
	s.cache.withLock(func(st *state.UserState) {
		s.accessor.del(st, name)
		s.cache.markDirty()
	})
	return nil
}
