package engine

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/state"
)

// --- Generic mock installer ---

type mockInstaller[R any, S any] struct {
	mu        sync.Mutex
	calls     []string
	installFn func(ctx context.Context, res R, name string) (S, error)
	removeFn  func(ctx context.Context, st S, name string) error
}

func (m *mockInstaller[R, S]) Install(ctx context.Context, res R, name string) (S, error) {
	m.mu.Lock()
	m.calls = append(m.calls, "Install:"+name)
	m.mu.Unlock()
	if m.installFn != nil {
		return m.installFn(ctx, res, name)
	}
	var zero S
	return zero, fmt.Errorf("installFn not set")
}

func (m *mockInstaller[R, S]) Remove(ctx context.Context, st S, name string) error {
	m.mu.Lock()
	m.calls = append(m.calls, "Remove:"+name)
	m.mu.Unlock()
	if m.removeFn != nil {
		return m.removeFn(ctx, st, name)
	}
	return nil
}

func (m *mockInstaller[R, S]) getCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.calls...)
}

// Concrete mock type aliases for readability.
type mockSysInstallerInstaller = mockInstaller[*resource.SystemInstaller, *resource.SystemInstallerState]
type mockSysRepoInstaller = mockInstaller[*resource.SystemPackageRepository, *resource.SystemPackageRepositoryState]
type mockSysPackageInstaller = mockInstaller[*resource.SystemPackageSet, *resource.SystemPackageSetState]

// --- Default install functions ---

func defaultInstallerInstallFn(_ context.Context, _ *resource.SystemInstaller, _ string) (*resource.SystemInstallerState, error) {
	return &resource.SystemInstallerState{Version: "1.0.0", UpdatedAt: time.Now()}, nil
}

func defaultRepoInstallFn(_ context.Context, res *resource.SystemPackageRepository, name string) (*resource.SystemPackageRepositoryState, error) {
	return &resource.SystemPackageRepositoryState{
		InstallerRef:   res.SystemPackageRepositorySpec.InstallerRef,
		Source:         res.SystemPackageRepositorySpec.Source,
		InstalledFiles: []string{"/usr/share/keyrings/" + name + ".gpg"},
		UpdatedAt:      time.Now(),
	}, nil
}

func defaultPackageInstallFn(_ context.Context, res *resource.SystemPackageSet, _ string) (*resource.SystemPackageSetState, error) {
	versions := make(map[string]string, len(res.SystemPackageSetSpec.Packages))
	for _, pkg := range res.SystemPackageSetSpec.Packages {
		versions[pkg] = "1.0.0"
	}
	return &resource.SystemPackageSetState{
		InstallerRef:      res.SystemPackageSetSpec.InstallerRef,
		RepositoryRef:     res.SystemPackageSetSpec.RepositoryRef,
		Packages:          res.SystemPackageSetSpec.Packages,
		InstalledVersions: versions,
		UpdatedAt:         time.Now(),
	}, nil
}

// --- Mock privilege handler ---

type mockPrivilegeHandler struct{}

func (*mockPrivilegeHandler) Acquire(context.Context) error { return nil }
func (*mockPrivilegeHandler) Release() error                { return nil }

// --- Test resource helpers ---

func testSystemInstaller(name string) *resource.SystemInstaller {
	return &resource.SystemInstaller{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindSystemInstaller,
			Metadata:     resource.Metadata{Name: name},
		},
		SystemInstallerSpec: &resource.SystemInstallerSpec{
			Pattern:    "delegation",
			Privileged: true,
			Commands: resource.SystemInstallerCommandsSpec{
				Install: resource.CommandSpec{Command: name, Verb: "install"},
				Remove:  resource.CommandSpec{Command: name, Verb: "remove"},
				Check:   resource.CommandSpec{Command: "dpkg", Verb: "-s"},
			},
		},
	}
}

func testSystemPackageRepository(name, installerRef string) *resource.SystemPackageRepository {
	return &resource.SystemPackageRepository{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindSystemPackageRepository,
			Metadata:     resource.Metadata{Name: name},
		},
		SystemPackageRepositorySpec: &resource.SystemPackageRepositorySpec{
			InstallerRef: installerRef,
			Source: resource.SourceConfig{
				URL:    "https://example.com/" + name + "/repo",
				KeyURL: "https://example.com/" + name + "/gpg",
			},
		},
	}
}

func testSystemPackageSet(name, installerRef, repoRef string, packages []string) *resource.SystemPackageSet {
	return &resource.SystemPackageSet{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindSystemPackageSet,
			Metadata:     resource.Metadata{Name: name},
		},
		SystemPackageSetSpec: &resource.SystemPackageSetSpec{
			InstallerRef:  installerRef,
			RepositoryRef: repoRef,
			Packages:      packages,
		},
	}
}

// --- Helper to create engine with mocks ---

type systemEngineTestSetup struct {
	engine        *SystemEngine
	store         *state.Store[state.SystemState]
	installerMock *mockSysInstallerInstaller
	repoMock      *mockSysRepoInstaller
	packageMock   *mockSysPackageInstaller
	events        []Event
	mu            sync.Mutex
}

func newSystemEngineTestSetup(t *testing.T) *systemEngineTestSetup {
	t.Helper()
	stateDir := t.TempDir()
	store, err := state.NewStore[state.SystemState](stateDir)
	require.NoError(t, err)

	installerMock := &mockSysInstallerInstaller{installFn: defaultInstallerInstallFn}
	repoMock := &mockSysRepoInstaller{installFn: defaultRepoInstallFn}
	packageMock := &mockSysPackageInstaller{installFn: defaultPackageInstallFn}

	engine := NewSystemEngine(installerMock, repoMock, packageMock, store)
	engine.SetPrivilegeHandler(&mockPrivilegeHandler{})

	s := &systemEngineTestSetup{
		engine:        engine,
		store:         store,
		installerMock: installerMock,
		repoMock:      repoMock,
		packageMock:   packageMock,
	}

	engine.SetEventHandler(func(event Event) {
		s.mu.Lock()
		s.events = append(s.events, event)
		s.mu.Unlock()
	})

	return s
}

func (s *systemEngineTestSetup) getEvents() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Event{}, s.events...)
}

// --- Tests ---

func TestNewSystemEngine(t *testing.T) {
	t.Parallel()
	s := newSystemEngineTestSetup(t)
	assert.NotNil(t, s.engine)
	assert.NotNil(t, s.engine.store)
	assert.NotNil(t, s.engine.stateCache)
	assert.NotNil(t, s.engine.installerReconciler)
	assert.NotNil(t, s.engine.repoReconciler)
	assert.NotNil(t, s.engine.packageReconciler)
	assert.NotNil(t, s.engine.installerExecutor)
	assert.NotNil(t, s.engine.repoExecutor)
	assert.NotNil(t, s.engine.packageExecutor)
}

func TestSystemEngine_Apply(t *testing.T) {
	t.Parallel()
	s := newSystemEngineTestSetup(t)

	resources := []resource.Resource{
		testSystemInstaller("apt"),
		testSystemPackageRepository("docker", "apt"),
		testSystemPackageSet("docker-pkgs", "apt", "docker", []string{"docker-ce", "containerd.io"}),
	}

	err := s.engine.Apply(context.Background(), resources)
	require.NoError(t, err)

	// Verify all installers were called
	assert.Equal(t, []string{"Install:apt"}, s.installerMock.getCalls())
	assert.Equal(t, []string{"Install:docker"}, s.repoMock.getCalls())
	assert.Equal(t, []string{"Install:docker-pkgs"}, s.packageMock.getCalls())

	// Verify state was persisted
	require.NoError(t, s.store.Lock())
	defer func() { _ = s.store.Unlock() }()
	st, err := s.store.Load()
	require.NoError(t, err)
	assert.NotNil(t, st.SystemInstallers["apt"])
	assert.NotNil(t, st.SystemPackageRepositories["docker"])
	assert.NotNil(t, st.SystemPackages["docker-pkgs"])
}

func TestSystemEngine_Apply_DAGOrder(t *testing.T) {
	t.Parallel()

	// Use a shared call log to track cross-installer ordering
	var mu sync.Mutex
	var allCalls []string
	recordCall := func(call string) {
		mu.Lock()
		allCalls = append(allCalls, call)
		mu.Unlock()
	}

	stateDir := t.TempDir()
	store, err := state.NewStore[state.SystemState](stateDir)
	require.NoError(t, err)

	installerMock := &mockSysInstallerInstaller{
		installFn: func(_ context.Context, _ *resource.SystemInstaller, name string) (*resource.SystemInstallerState, error) {
			recordCall("installer:" + name)
			return &resource.SystemInstallerState{Version: "1.0.0", UpdatedAt: time.Now()}, nil
		},
	}
	repoMock := &mockSysRepoInstaller{
		installFn: func(_ context.Context, res *resource.SystemPackageRepository, name string) (*resource.SystemPackageRepositoryState, error) {
			recordCall("repo:" + name)
			return &resource.SystemPackageRepositoryState{
				InstallerRef: res.SystemPackageRepositorySpec.InstallerRef,
				Source:       res.SystemPackageRepositorySpec.Source,
				UpdatedAt:    time.Now(),
			}, nil
		},
	}
	packageMock := &mockSysPackageInstaller{
		installFn: func(_ context.Context, res *resource.SystemPackageSet, name string) (*resource.SystemPackageSetState, error) {
			recordCall("package:" + name)
			return &resource.SystemPackageSetState{
				InstallerRef:  res.SystemPackageSetSpec.InstallerRef,
				RepositoryRef: res.SystemPackageSetSpec.RepositoryRef,
				Packages:      res.SystemPackageSetSpec.Packages,
				UpdatedAt:     time.Now(),
			}, nil
		},
	}

	engine := NewSystemEngine(installerMock, repoMock, packageMock, store)
	engine.SetPrivilegeHandler(&mockPrivilegeHandler{})

	resources := []resource.Resource{
		testSystemInstaller("apt"),
		testSystemPackageRepository("docker", "apt"),
		testSystemPackageSet("docker-pkgs", "apt", "docker", []string{"docker-ce"}),
	}

	err = engine.Apply(context.Background(), resources)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, allCalls, 3)
	assert.Equal(t, "installer:apt", allCalls[0])
	assert.Equal(t, "repo:docker", allCalls[1])
	assert.Equal(t, "package:docker-pkgs", allCalls[2])
}

func TestSystemEngine_Apply_NoChanges(t *testing.T) {
	t.Parallel()
	s := newSystemEngineTestSetup(t)

	// Pre-populate state
	require.NoError(t, s.store.Lock())
	st := state.NewSystemState()
	st.SystemInstallers["apt"] = &resource.SystemInstallerState{
		Version:   "1.0.0",
		UpdatedAt: time.Now(),
	}
	st.SystemPackageRepositories["docker"] = &resource.SystemPackageRepositoryState{
		InstallerRef: "apt",
		Source: resource.SourceConfig{
			URL:    "https://example.com/docker/repo",
			KeyURL: "https://example.com/docker/gpg",
		},
		UpdatedAt: time.Now(),
	}
	st.SystemPackages["docker-pkgs"] = &resource.SystemPackageSetState{
		InstallerRef:  "apt",
		RepositoryRef: "docker",
		Packages:      []string{"docker-ce", "containerd.io"},
		UpdatedAt:     time.Now(),
	}
	require.NoError(t, s.store.Save(st))
	require.NoError(t, s.store.Unlock())

	resources := []resource.Resource{
		testSystemInstaller("apt"),
		testSystemPackageRepository("docker", "apt"),
		testSystemPackageSet("docker-pkgs", "apt", "docker", []string{"docker-ce", "containerd.io"}),
	}

	err := s.engine.Apply(context.Background(), resources)
	require.NoError(t, err)

	// No install calls should have been made
	assert.Empty(t, s.installerMock.getCalls())
	assert.Empty(t, s.repoMock.getCalls())
	assert.Empty(t, s.packageMock.getCalls())
}

func TestSystemEngine_Apply_Removals(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var removeCalls []string

	stateDir := t.TempDir()
	store, err := state.NewStore[state.SystemState](stateDir)
	require.NoError(t, err)

	// Pre-populate state with resources that will be removed
	require.NoError(t, store.Lock())
	st := state.NewSystemState()
	st.SystemInstallers["apt"] = &resource.SystemInstallerState{
		Version:   "1.0.0",
		UpdatedAt: time.Now(),
	}
	st.SystemPackageRepositories["docker"] = &resource.SystemPackageRepositoryState{
		InstallerRef: "apt",
		Source:       resource.SourceConfig{URL: "https://example.com/docker/repo"},
		UpdatedAt:    time.Now(),
	}
	st.SystemPackages["docker-pkgs"] = &resource.SystemPackageSetState{
		InstallerRef:  "apt",
		RepositoryRef: "docker",
		Packages:      []string{"docker-ce"},
		UpdatedAt:     time.Now(),
	}
	require.NoError(t, store.Save(st))
	require.NoError(t, store.Unlock())

	installerMock := &mockSysInstallerInstaller{
		installFn: defaultInstallerInstallFn,
		removeFn: func(_ context.Context, _ *resource.SystemInstallerState, name string) error {
			mu.Lock()
			removeCalls = append(removeCalls, "installer:"+name)
			mu.Unlock()
			return nil
		},
	}
	repoMock := &mockSysRepoInstaller{
		installFn: defaultRepoInstallFn,
		removeFn: func(_ context.Context, _ *resource.SystemPackageRepositoryState, name string) error {
			mu.Lock()
			removeCalls = append(removeCalls, "repo:"+name)
			mu.Unlock()
			return nil
		},
	}
	packageMock := &mockSysPackageInstaller{
		installFn: defaultPackageInstallFn,
		removeFn: func(_ context.Context, _ *resource.SystemPackageSetState, name string) error {
			mu.Lock()
			removeCalls = append(removeCalls, "package:"+name)
			mu.Unlock()
			return nil
		},
	}

	engine := NewSystemEngine(installerMock, repoMock, packageMock, store)
	engine.SetPrivilegeHandler(&mockPrivilegeHandler{})

	// Apply with empty resources → all should be removed
	err = engine.Apply(context.Background(), []resource.Resource{})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	// Reverse order: packages → repos → installers
	require.Len(t, removeCalls, 3)
	assert.Equal(t, "package:docker-pkgs", removeCalls[0])
	assert.Equal(t, "repo:docker", removeCalls[1])
	assert.Equal(t, "installer:apt", removeCalls[2])

	// Verify state is cleared
	require.NoError(t, store.Lock())
	defer func() { _ = store.Unlock() }()
	st, err = store.Load()
	require.NoError(t, err)
	assert.Empty(t, st.SystemInstallers)
	assert.Empty(t, st.SystemPackageRepositories)
	assert.Empty(t, st.SystemPackages)
}

func TestSystemEngine_Apply_RequiresPrivilegeHandler(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	store, err := state.NewStore[state.SystemState](stateDir)
	require.NoError(t, err)

	engine := NewSystemEngine(
		&mockSysInstallerInstaller{installFn: defaultInstallerInstallFn},
		&mockSysRepoInstaller{installFn: defaultRepoInstallFn},
		&mockSysPackageInstaller{installFn: defaultPackageInstallFn},
		store,
	)
	// Do NOT set privilege handler

	resources := []resource.Resource{
		testSystemInstaller("apt"),
	}

	err = engine.Apply(context.Background(), resources)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "privilege")
}

func TestSystemEngine_Apply_ContextCancellation(t *testing.T) {
	t.Parallel()
	s := newSystemEngineTestSetup(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	resources := []resource.Resource{
		testSystemInstaller("apt"),
	}

	err := s.engine.Apply(ctx, resources)
	require.Error(t, err)

	// No installer should have been called
	assert.Empty(t, s.installerMock.getCalls())
}

func TestSystemEngine_Apply_Events(t *testing.T) {
	t.Parallel()
	s := newSystemEngineTestSetup(t)

	resources := []resource.Resource{
		testSystemInstaller("apt"),
		testSystemPackageRepository("docker", "apt"),
	}

	err := s.engine.Apply(context.Background(), resources)
	require.NoError(t, err)

	events := s.getEvents()

	// Check for start and complete events for each resource
	var starts, completes []Event
	for _, e := range events {
		switch e.Type {
		case EventStart:
			starts = append(starts, e)
		case EventComplete:
			completes = append(completes, e)
		}
	}

	require.Len(t, starts, 2)
	require.Len(t, completes, 2)

	// Verify kinds are correct
	assert.Equal(t, resource.KindSystemInstaller, starts[0].Kind)
	assert.Equal(t, resource.KindSystemPackageRepository, starts[1].Kind)
}

func TestSystemEngine_Apply_UpgradeRepo(t *testing.T) {
	t.Parallel()
	s := newSystemEngineTestSetup(t)

	// Pre-populate state with old URL
	require.NoError(t, s.store.Lock())
	st := state.NewSystemState()
	st.SystemInstallers["apt"] = &resource.SystemInstallerState{
		Version:   "1.0.0",
		UpdatedAt: time.Now(),
	}
	st.SystemPackageRepositories["docker"] = &resource.SystemPackageRepositoryState{
		InstallerRef: "apt",
		Source: resource.SourceConfig{
			URL:    "https://old.example.com/docker/repo",
			KeyURL: "https://example.com/docker/gpg",
		},
		UpdatedAt: time.Now(),
	}
	require.NoError(t, s.store.Save(st))
	require.NoError(t, s.store.Unlock())

	resources := []resource.Resource{
		testSystemInstaller("apt"),
		testSystemPackageRepository("docker", "apt"),
	}

	err := s.engine.Apply(context.Background(), resources)
	require.NoError(t, err)

	// Installer should not be called (no change), repo should be called (URL changed)
	assert.Empty(t, s.installerMock.getCalls())
	assert.Equal(t, []string{"Install:docker"}, s.repoMock.getCalls())

	// Verify updated state
	require.NoError(t, s.store.Lock())
	defer func() { _ = s.store.Unlock() }()
	updatedSt, err := s.store.Load()
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/docker/repo", updatedSt.SystemPackageRepositories["docker"].Source.URL)
}

func TestSystemEngine_Apply_MultipleInstallers(t *testing.T) {
	t.Parallel()
	s := newSystemEngineTestSetup(t)

	resources := []resource.Resource{
		testSystemInstaller("apt"),
		testSystemInstaller("dnf"),
		testSystemPackageRepository("docker", "apt"),
		testSystemPackageRepository("kubernetes", "dnf"),
		testSystemPackageSet("docker-pkgs", "apt", "docker", []string{"docker-ce"}),
		testSystemPackageSet("k8s-pkgs", "dnf", "kubernetes", []string{"kubectl", "kubelet"}),
	}

	err := s.engine.Apply(context.Background(), resources)
	require.NoError(t, err)

	// Verify state was persisted for all resources
	require.NoError(t, s.store.Lock())
	defer func() { _ = s.store.Unlock() }()
	st, err := s.store.Load()
	require.NoError(t, err)
	assert.Len(t, st.SystemInstallers, 2)
	assert.Len(t, st.SystemPackageRepositories, 2)
	assert.Len(t, st.SystemPackages, 2)
}

func TestSystemEngine_Apply_ErrorMidLayer(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	store, err := state.NewStore[state.SystemState](stateDir)
	require.NoError(t, err)

	// Repo installer fails
	repoMock := &mockSysRepoInstaller{
		installFn: func(_ context.Context, _ *resource.SystemPackageRepository, _ string) (*resource.SystemPackageRepositoryState, error) {
			return nil, fmt.Errorf("repo install failed")
		},
	}

	engine := NewSystemEngine(
		&mockSysInstallerInstaller{installFn: defaultInstallerInstallFn},
		repoMock,
		&mockSysPackageInstaller{installFn: defaultPackageInstallFn},
		store,
	)
	engine.SetPrivilegeHandler(&mockPrivilegeHandler{})

	resources := []resource.Resource{
		testSystemInstaller("apt"),
		testSystemPackageRepository("docker", "apt"),
		testSystemPackageSet("docker-pkgs", "apt", "docker", []string{"docker-ce"}),
	}

	err = engine.Apply(context.Background(), resources)
	require.Error(t, err)

	// The installer layer (apt) should have been persisted before the repo layer error
	require.NoError(t, store.Lock())
	defer func() { _ = store.Unlock() }()
	st, err := store.Load()
	require.NoError(t, err)
	assert.NotNil(t, st.SystemInstallers["apt"], "installer state should be persisted before repo layer error")
	assert.Nil(t, st.SystemPackageRepositories["docker"], "failed repo should not be in state")
}

func TestSystemEngine_Apply_EmptyResources(t *testing.T) {
	t.Parallel()
	s := newSystemEngineTestSetup(t)

	err := s.engine.Apply(context.Background(), []resource.Resource{})
	require.NoError(t, err)

	assert.Empty(t, s.installerMock.getCalls())
	assert.Empty(t, s.repoMock.getCalls())
	assert.Empty(t, s.packageMock.getCalls())
}

func TestSystemEngine_PlanAll(t *testing.T) {
	t.Parallel()
	s := newSystemEngineTestSetup(t)

	resources := []resource.Resource{
		testSystemInstaller("apt"),
		testSystemPackageRepository("docker", "apt"),
		testSystemPackageSet("docker-pkgs", "apt", "docker", []string{"docker-ce"}),
	}

	installerActions, repoActions, packageActions, err := s.engine.PlanAll(context.Background(), resources)
	require.NoError(t, err)

	// All should be install actions (no prior state)
	require.Len(t, installerActions, 1)
	assert.Equal(t, resource.ActionInstall, installerActions[0].Type)
	assert.Equal(t, "apt", installerActions[0].Name)

	require.Len(t, repoActions, 1)
	assert.Equal(t, resource.ActionInstall, repoActions[0].Type)
	assert.Equal(t, "docker", repoActions[0].Name)

	require.Len(t, packageActions, 1)
	assert.Equal(t, resource.ActionInstall, packageActions[0].Type)
	assert.Equal(t, "docker-pkgs", packageActions[0].Name)
}

func TestSystemEngine_PlanAll_NoChanges(t *testing.T) {
	t.Parallel()
	s := newSystemEngineTestSetup(t)

	// Pre-populate state matching spec
	require.NoError(t, s.store.Lock())
	st := state.NewSystemState()
	st.SystemInstallers["apt"] = &resource.SystemInstallerState{
		Version:   "1.0.0",
		UpdatedAt: time.Now(),
	}
	st.SystemPackageRepositories["docker"] = &resource.SystemPackageRepositoryState{
		InstallerRef: "apt",
		Source: resource.SourceConfig{
			URL:    "https://example.com/docker/repo",
			KeyURL: "https://example.com/docker/gpg",
		},
		UpdatedAt: time.Now(),
	}
	st.SystemPackages["docker-pkgs"] = &resource.SystemPackageSetState{
		InstallerRef:  "apt",
		RepositoryRef: "docker",
		Packages:      []string{"docker-ce"},
		UpdatedAt:     time.Now(),
	}
	require.NoError(t, s.store.Save(st))
	require.NoError(t, s.store.Unlock())

	resources := []resource.Resource{
		testSystemInstaller("apt"),
		testSystemPackageRepository("docker", "apt"),
		testSystemPackageSet("docker-pkgs", "apt", "docker", []string{"docker-ce"}),
	}

	installerActions, repoActions, packageActions, err := s.engine.PlanAll(context.Background(), resources)
	require.NoError(t, err)

	assert.Empty(t, installerActions)
	assert.Empty(t, repoActions)
	assert.Empty(t, packageActions)
}

func TestSystemEngine_PlanAll_InstallerRemovalDependencyError(t *testing.T) {
	t.Parallel()
	s := newSystemEngineTestSetup(t)

	// Pre-populate state: installer "apt" exists, repo "docker" depends on it
	require.NoError(t, s.store.Lock())
	st := state.NewSystemState()
	st.SystemInstallers["apt"] = &resource.SystemInstallerState{
		Version:   "1.0.0",
		UpdatedAt: time.Now(),
	}
	st.SystemPackageRepositories["docker"] = &resource.SystemPackageRepositoryState{
		InstallerRef: "apt",
		Source:       resource.SourceConfig{URL: "https://example.com/docker/repo"},
		UpdatedAt:    time.Now(),
	}
	require.NoError(t, s.store.Save(st))
	require.NoError(t, s.store.Unlock())

	// Spec keeps repo "docker" but removes installer "apt" → dependency error
	resources := []resource.Resource{
		testSystemPackageRepository("docker", "apt"),
	}

	_, _, _, err := s.engine.PlanAll(context.Background(), resources)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "apt")
}

func TestSystemEngine_PlanAll_RepoRemovalDependencyError(t *testing.T) {
	t.Parallel()
	s := newSystemEngineTestSetup(t)

	// Pre-populate state: repo "docker" exists, package set depends on it
	require.NoError(t, s.store.Lock())
	st := state.NewSystemState()
	st.SystemInstallers["apt"] = &resource.SystemInstallerState{
		Version:   "1.0.0",
		UpdatedAt: time.Now(),
	}
	st.SystemPackageRepositories["docker"] = &resource.SystemPackageRepositoryState{
		InstallerRef: "apt",
		Source:       resource.SourceConfig{URL: "https://example.com/docker/repo"},
		UpdatedAt:    time.Now(),
	}
	st.SystemPackages["docker-pkgs"] = &resource.SystemPackageSetState{
		InstallerRef:  "apt",
		RepositoryRef: "docker",
		Packages:      []string{"docker-ce"},
		UpdatedAt:     time.Now(),
	}
	require.NoError(t, s.store.Save(st))
	require.NoError(t, s.store.Unlock())

	// Spec keeps installer and packages but removes repo → dependency error
	resources := []resource.Resource{
		testSystemInstaller("apt"),
		testSystemPackageSet("docker-pkgs", "apt", "docker", []string{"docker-ce"}),
	}

	_, _, _, err := s.engine.PlanAll(context.Background(), resources)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "docker")
}

func TestSystemEngine_Apply_RemoveError(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	store, err := state.NewStore[state.SystemState](stateDir)
	require.NoError(t, err)

	// Pre-populate state with a single package set
	require.NoError(t, store.Lock())
	st := state.NewSystemState()
	st.SystemPackages["failing-pkg"] = &resource.SystemPackageSetState{
		InstallerRef: "apt",
		Packages:     []string{"a"},
		UpdatedAt:    time.Now(),
	}
	require.NoError(t, store.Save(st))
	require.NoError(t, store.Unlock())

	packageMock := &mockSysPackageInstaller{
		installFn: defaultPackageInstallFn,
		removeFn: func(_ context.Context, _ *resource.SystemPackageSetState, name string) error {
			return fmt.Errorf("remove failed for %s", name)
		},
	}

	engine := NewSystemEngine(
		&mockSysInstallerInstaller{installFn: defaultInstallerInstallFn},
		&mockSysRepoInstaller{installFn: defaultRepoInstallFn},
		packageMock,
		store,
	)
	engine.SetPrivilegeHandler(&mockPrivilegeHandler{})

	// Apply with empty resources → removal should fail
	err = engine.Apply(context.Background(), []resource.Resource{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failing-pkg")

	// State should still contain the failed resource
	require.NoError(t, store.Lock())
	defer func() { _ = store.Unlock() }()
	st, err = store.Load()
	require.NoError(t, err)
	assert.NotNil(t, st.SystemPackages["failing-pkg"], "failed removal should leave state intact")
}

func TestSystemEngine_Apply_RemoveErrorFlushesSuccessful(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	store, err := state.NewStore[state.SystemState](stateDir)
	require.NoError(t, err)

	// Pre-populate state: one package set (will be removed successfully)
	// and one repo (removal will fail).
	// Removal order is packages → repos → installers, so packages should
	// be removed and flushed before the repo removal fails.
	require.NoError(t, store.Lock())
	st := state.NewSystemState()
	st.SystemPackages["good-pkg"] = &resource.SystemPackageSetState{
		InstallerRef: "apt",
		Packages:     []string{"a"},
		UpdatedAt:    time.Now(),
	}
	st.SystemPackageRepositories["bad-repo"] = &resource.SystemPackageRepositoryState{
		InstallerRef: "apt",
		Source:       resource.SourceConfig{URL: "https://example.com"},
		UpdatedAt:    time.Now(),
	}
	require.NoError(t, store.Save(st))
	require.NoError(t, store.Unlock())

	repoMock := &mockSysRepoInstaller{
		installFn: defaultRepoInstallFn,
		removeFn: func(_ context.Context, _ *resource.SystemPackageRepositoryState, name string) error {
			return fmt.Errorf("remove failed for %s", name)
		},
	}

	engine := NewSystemEngine(
		&mockSysInstallerInstaller{installFn: defaultInstallerInstallFn},
		repoMock,
		&mockSysPackageInstaller{installFn: defaultPackageInstallFn},
		store,
	)
	engine.SetPrivilegeHandler(&mockPrivilegeHandler{})

	// Apply with empty resources → repo removal should fail
	err = engine.Apply(context.Background(), []resource.Resource{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad-repo")

	// Verify: successful package removal was flushed despite the repo error
	require.NoError(t, store.Lock())
	defer func() { _ = store.Unlock() }()
	st, err = store.Load()
	require.NoError(t, err)
	assert.Nil(t, st.SystemPackages["good-pkg"], "successful removal should be persisted even when later batch fails")
	assert.NotNil(t, st.SystemPackageRepositories["bad-repo"], "failed removal should leave state intact")
}
