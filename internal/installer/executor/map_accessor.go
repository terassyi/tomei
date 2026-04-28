package executor

import (
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/state"
)

// mapAccessor abstracts access to a specific map within a state type.
// T is the container (UserState or SystemState), S is the value type.
// Each implementation knows only about its own map field.
type mapAccessor[T state.State, S resource.State] interface {
	get(st *T, name string) (S, bool)
	set(st *T, name string, val S)
	del(st *T, name string)
}

// Compile-time interface satisfaction checks.
var (
	_ mapAccessor[state.UserState, *resource.ToolState]                = toolMapAccessor{}
	_ mapAccessor[state.UserState, *resource.RuntimeState]             = runtimeMapAccessor{}
	_ mapAccessor[state.UserState, *resource.InstallerRepositoryState] = repoMapAccessor{}
)

// --- Tool ---

type toolMapAccessor struct{}

func (toolMapAccessor) get(st *state.UserState, name string) (*resource.ToolState, bool) {
	if st.Tools == nil {
		return nil, false
	}
	v, ok := st.Tools[name]
	return v, ok
}

func (toolMapAccessor) set(st *state.UserState, name string, val *resource.ToolState) {
	if st.Tools == nil {
		st.Tools = make(map[string]*resource.ToolState)
	}
	st.Tools[name] = val
}

func (toolMapAccessor) del(st *state.UserState, name string) {
	delete(st.Tools, name)
}

// NewToolStore creates a StateStore for tool state backed by the given cache.
func NewToolStore(cache *StateCache[state.UserState]) StateStore[*resource.ToolState] {
	return &cachedStore[state.UserState, *resource.ToolState]{cache: cache, accessor: toolMapAccessor{}}
}

// --- Runtime ---

type runtimeMapAccessor struct{}

func (runtimeMapAccessor) get(st *state.UserState, name string) (*resource.RuntimeState, bool) {
	if st.Runtimes == nil {
		return nil, false
	}
	v, ok := st.Runtimes[name]
	return v, ok
}

func (runtimeMapAccessor) set(st *state.UserState, name string, val *resource.RuntimeState) {
	if st.Runtimes == nil {
		st.Runtimes = make(map[string]*resource.RuntimeState)
	}
	st.Runtimes[name] = val
}

func (runtimeMapAccessor) del(st *state.UserState, name string) {
	delete(st.Runtimes, name)
}

// NewRuntimeStore creates a StateStore for runtime state backed by the given cache.
func NewRuntimeStore(cache *StateCache[state.UserState]) StateStore[*resource.RuntimeState] {
	return &cachedStore[state.UserState, *resource.RuntimeState]{cache: cache, accessor: runtimeMapAccessor{}}
}

// --- InstallerRepository ---

type repoMapAccessor struct{}

func (repoMapAccessor) get(st *state.UserState, name string) (*resource.InstallerRepositoryState, bool) {
	if st.InstallerRepositories == nil {
		return nil, false
	}
	v, ok := st.InstallerRepositories[name]
	return v, ok
}

func (repoMapAccessor) set(st *state.UserState, name string, val *resource.InstallerRepositoryState) {
	if st.InstallerRepositories == nil {
		st.InstallerRepositories = make(map[string]*resource.InstallerRepositoryState)
	}
	st.InstallerRepositories[name] = val
}

func (repoMapAccessor) del(st *state.UserState, name string) {
	delete(st.InstallerRepositories, name)
}

// NewInstallerRepositoryStore creates a StateStore for installer repository state backed by the given cache.
func NewInstallerRepositoryStore(cache *StateCache[state.UserState]) StateStore[*resource.InstallerRepositoryState] {
	return &cachedStore[state.UserState, *resource.InstallerRepositoryState]{cache: cache, accessor: repoMapAccessor{}}
}
