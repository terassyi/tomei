package executor

import (
	"sync"

	"github.com/terassyi/tomei/internal/state"
)

// StateStoreFactory creates StateStore instances that share a single mutex.
// This ensures that ToolStateStore and RuntimeStateStore, which both
// read/write the same state.json, are properly serialized.
type StateStoreFactory struct {
	mu    sync.Mutex
	store *state.Store[state.UserState]
}

// NewStateStoreFactory creates a new StateStoreFactory.
func NewStateStoreFactory(store *state.Store[state.UserState]) *StateStoreFactory {
	return &StateStoreFactory{store: store}
}

// ToolStore creates a ToolStateStore with the shared mutex.
func (f *StateStoreFactory) ToolStore() *ToolStateStore {
	return &ToolStateStore{mu: &f.mu, store: f.store}
}

// RuntimeStore creates a RuntimeStateStore with the shared mutex.
func (f *StateStoreFactory) RuntimeStore() *RuntimeStateStore {
	return &RuntimeStateStore{mu: &f.mu, store: f.store}
}

// InstallerRepositoryStore creates an InstallerRepositoryStateStore with the shared mutex.
func (f *StateStoreFactory) InstallerRepositoryStore() *InstallerRepositoryStateStore {
	return &InstallerRepositoryStateStore{mu: &f.mu, store: f.store}
}
