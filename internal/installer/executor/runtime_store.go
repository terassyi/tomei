package executor

import (
	"sync"

	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/state"
)

// RuntimeStateStore adapts state.Store to the StateStore interface for Runtimes.
type RuntimeStateStore struct {
	mu    *sync.Mutex
	store *state.Store[state.UserState]
}

// Load loads a runtime state by name.
func (s *RuntimeStateStore) Load(name string) (*resource.RuntimeState, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.store.Load()
	if err != nil {
		return nil, false, err
	}
	runtimeState, exists := st.Runtimes[name]
	return runtimeState, exists, nil
}

// Save saves a runtime state.
func (s *RuntimeStateStore) Save(name string, runtimeState *resource.RuntimeState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.store.Load()
	if err != nil {
		return err
	}

	if st.Runtimes == nil {
		st.Runtimes = make(map[string]*resource.RuntimeState)
	}
	st.Runtimes[name] = runtimeState

	return s.store.Save(st)
}

// Delete removes a runtime from state.
func (s *RuntimeStateStore) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.store.Load()
	if err != nil {
		return err
	}

	delete(st.Runtimes, name)

	return s.store.Save(st)
}
