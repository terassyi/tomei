package executor

import (
	"sync"

	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/state"
)

// ToolStateStore adapts state.Store to the StateStore interface for Tools.
type ToolStateStore struct {
	mu    *sync.Mutex
	store *state.Store[state.UserState]
}

// Load loads a tool state by name.
func (s *ToolStateStore) Load(name string) (*resource.ToolState, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.store.Load()
	if err != nil {
		return nil, false, err
	}
	toolState, exists := st.Tools[name]
	return toolState, exists, nil
}

// Save saves a tool state.
func (s *ToolStateStore) Save(name string, toolState *resource.ToolState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.store.Load()
	if err != nil {
		return err
	}

	if st.Tools == nil {
		st.Tools = make(map[string]*resource.ToolState)
	}
	st.Tools[name] = toolState

	return s.store.Save(st)
}

// Delete removes a tool from state.
func (s *ToolStateStore) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.store.Load()
	if err != nil {
		return err
	}

	delete(st.Tools, name)

	return s.store.Save(st)
}
