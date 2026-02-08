package executor

import (
	"sync"

	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/state"
)

// InstallerRepositoryStateStore adapts state.Store to the StateStore interface for InstallerRepositories.
type InstallerRepositoryStateStore struct {
	mu    *sync.Mutex
	store *state.Store[state.UserState]
}

// Load loads an installer repository state by name.
func (s *InstallerRepositoryStateStore) Load(name string) (*resource.InstallerRepositoryState, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.store.Load()
	if err != nil {
		return nil, false, err
	}
	repoState, exists := st.InstallerRepositories[name]
	return repoState, exists, nil
}

// Save saves an installer repository state.
func (s *InstallerRepositoryStateStore) Save(name string, repoState *resource.InstallerRepositoryState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.store.Load()
	if err != nil {
		return err
	}

	if st.InstallerRepositories == nil {
		st.InstallerRepositories = make(map[string]*resource.InstallerRepositoryState)
	}
	st.InstallerRepositories[name] = repoState

	return s.store.Save(st)
}

// Delete removes an installer repository from state.
func (s *InstallerRepositoryStateStore) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.store.Load()
	if err != nil {
		return err
	}

	delete(st.InstallerRepositories, name)

	return s.store.Save(st)
}
