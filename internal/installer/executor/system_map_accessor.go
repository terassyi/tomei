package executor

import (
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/state"
)

// --- SystemInstaller ---

type systemInstallerMapAccessor struct{}

func (systemInstallerMapAccessor) get(st *state.SystemState, name string) (*resource.SystemInstallerState, bool) {
	if st.SystemInstallers == nil {
		return nil, false
	}
	v, ok := st.SystemInstallers[name]
	return v, ok
}

func (systemInstallerMapAccessor) set(st *state.SystemState, name string, val *resource.SystemInstallerState) {
	if st.SystemInstallers == nil {
		st.SystemInstallers = make(map[string]*resource.SystemInstallerState)
	}
	st.SystemInstallers[name] = val
}

func (systemInstallerMapAccessor) del(st *state.SystemState, name string) {
	delete(st.SystemInstallers, name)
}

// NewSystemInstallerStore creates a StateStore for system installer state backed by the given cache.
func NewSystemInstallerStore(cache *StateCache[state.SystemState]) StateStore[*resource.SystemInstallerState] {
	return &cachedStore[state.SystemState, *resource.SystemInstallerState]{cache: cache, accessor: systemInstallerMapAccessor{}}
}

// --- SystemPackageRepository ---

type systemPackageRepoMapAccessor struct{}

func (systemPackageRepoMapAccessor) get(st *state.SystemState, name string) (*resource.SystemPackageRepositoryState, bool) {
	if st.SystemPackageRepositories == nil {
		return nil, false
	}
	v, ok := st.SystemPackageRepositories[name]
	return v, ok
}

func (systemPackageRepoMapAccessor) set(st *state.SystemState, name string, val *resource.SystemPackageRepositoryState) {
	if st.SystemPackageRepositories == nil {
		st.SystemPackageRepositories = make(map[string]*resource.SystemPackageRepositoryState)
	}
	st.SystemPackageRepositories[name] = val
}

func (systemPackageRepoMapAccessor) del(st *state.SystemState, name string) {
	delete(st.SystemPackageRepositories, name)
}

// NewSystemPackageRepositoryStore creates a StateStore for system package repository state backed by the given cache.
func NewSystemPackageRepositoryStore(cache *StateCache[state.SystemState]) StateStore[*resource.SystemPackageRepositoryState] {
	return &cachedStore[state.SystemState, *resource.SystemPackageRepositoryState]{cache: cache, accessor: systemPackageRepoMapAccessor{}}
}

// --- SystemPackageSet ---

type systemPackageSetMapAccessor struct{}

func (systemPackageSetMapAccessor) get(st *state.SystemState, name string) (*resource.SystemPackageSetState, bool) {
	if st.SystemPackages == nil {
		return nil, false
	}
	v, ok := st.SystemPackages[name]
	return v, ok
}

func (systemPackageSetMapAccessor) set(st *state.SystemState, name string, val *resource.SystemPackageSetState) {
	if st.SystemPackages == nil {
		st.SystemPackages = make(map[string]*resource.SystemPackageSetState)
	}
	st.SystemPackages[name] = val
}

func (systemPackageSetMapAccessor) del(st *state.SystemState, name string) {
	delete(st.SystemPackages, name)
}

// NewSystemPackageSetStore creates a StateStore for system package set state backed by the given cache.
func NewSystemPackageSetStore(cache *StateCache[state.SystemState]) StateStore[*resource.SystemPackageSetState] {
	return &cachedStore[state.SystemState, *resource.SystemPackageSetState]{cache: cache, accessor: systemPackageSetMapAccessor{}}
}
