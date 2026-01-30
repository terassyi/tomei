package executor

import (
	"github.com/terassyi/toto/internal/resource"
	"github.com/terassyi/toto/internal/state"
)

// ToolStateStore adapts state.Store to the StateStore interface for Tools.
type ToolStateStore struct {
	store *state.Store[state.UserState]
}

// NewToolStateStore creates a new ToolStateStore.
func NewToolStateStore(store *state.Store[state.UserState]) *ToolStateStore {
	return &ToolStateStore{store: store}
}

// Load loads a tool state by name.
func (s *ToolStateStore) Load(name string) (*resource.ToolState, bool, error) {
	st, err := s.store.Load()
	if err != nil {
		return nil, false, err
	}
	toolState, exists := st.Tools[name]
	return toolState, exists, nil
}

// Save saves a tool state.
func (s *ToolStateStore) Save(name string, toolState *resource.ToolState) error {
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
	st, err := s.store.Load()
	if err != nil {
		return err
	}

	delete(st.Tools, name)

	return s.store.Save(st)
}
