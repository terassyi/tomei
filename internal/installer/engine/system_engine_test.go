package engine

import (
	"context"
	"fmt"
	"maps"
	"slices"
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

// cloneAptSource returns a deep copy of spec.Apt with independent slice/map
// backing, matching the slices.Clone / maps.Clone invariant maintained by the
// real apt.PackageRepositoryInstaller. Centralizing this so that adding a new
// AptSource field (e.g., SignedBy, Architectures) requires only one edit and
// cannot leave a shallow-mock site behind that silently hides regressions.
// Returns nil when src is nil.
func cloneAptSource(src *resource.AptSource) *resource.AptSource {
	if src == nil {
		return nil
	}
	return &resource.AptSource{
		URL:        src.URL,
		KeyURL:     src.KeyURL,
		KeyHash:    src.KeyHash,
		Suite:      src.Suite,
		Components: slices.Clone(src.Components),
		Options:    maps.Clone(src.Options),
	}
}

func defaultRepoInstallFn(_ context.Context, res *resource.SystemPackageRepository, name string) (*resource.SystemPackageRepositoryState, error) {
	// Match the real apt PackageRepositoryInstaller contract on two
	// axes:
	//   1. state.InstalledFiles records the paths placed on disk
	//      ([keyring, sources.list]). Remove uses InstalledFiles as a
	//      membership set for path validation and then deletes in a
	//      canonical sequence regardless of slice order — recording
	//      both paths keeps engine tests honest about that contract
	//      (a stub that only set one would silently hide ordering /
	//      reverse-iteration regressions in the real installer).
	//   2. state.Apt is a deep copy of spec.Apt (see cloneAptSource).
	return &resource.SystemPackageRepositoryState{
		InstallerRef: res.SystemPackageRepositorySpec.InstallerRef,
		InstalledFiles: []string{
			"/usr/share/keyrings/" + name + ".gpg",
			"/etc/apt/sources.list.d/" + name + ".list",
		},
		Apt:       cloneAptSource(res.SystemPackageRepositorySpec.Apt),
		UpdatedAt: time.Now(),
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

// --- Call recorder for ordering tests ---

// callRecorder bundles the three System installer mocks wired to a shared,
// lock-guarded call log. Each mock records "<kind>:<name>" before delegating
// to its installFn — the "<kind>:<name>" format is the public contract and
// tests assert against it directly.
//
// Defaults:
//   - installer returns a stub SystemInstallerState
//   - repo returns a deep-copied repo state via cloneAptSource
//   - pkg returns a stub SystemPackageSetState
//
// Failure injection: tests REPLACE (not chain) installFn on the relevant mock
// before passing the mocks to NewSystemEngine. The replacement is responsible
// for calling .record itself — this keeps the failure-mode Given visible at
// the call site.
type callRecorder struct {
	installer *mockSysInstallerInstaller
	repo      *mockSysRepoInstaller
	pkg       *mockSysPackageInstaller
	record    func(string)    // append to the log; safe under t.Parallel
	snapshot  func() []string // lock-safe copy of the log
}

func newCallRecorder() *callRecorder {
	var mu sync.Mutex
	var allCalls []string
	record := func(call string) {
		mu.Lock()
		allCalls = append(allCalls, call)
		mu.Unlock()
	}
	snapshot := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), allCalls...)
	}

	return &callRecorder{
		record:   record,
		snapshot: snapshot,
		installer: &mockSysInstallerInstaller{
			installFn: func(_ context.Context, _ *resource.SystemInstaller, name string) (*resource.SystemInstallerState, error) {
				record("installer:" + name)
				return &resource.SystemInstallerState{Version: "1.0.0", UpdatedAt: time.Now()}, nil
			},
		},
		repo: &mockSysRepoInstaller{
			installFn: func(_ context.Context, res *resource.SystemPackageRepository, name string) (*resource.SystemPackageRepositoryState, error) {
				record("repo:" + name)
				return &resource.SystemPackageRepositoryState{
					InstallerRef: res.SystemPackageRepositorySpec.InstallerRef,
					Apt:          cloneAptSource(res.SystemPackageRepositorySpec.Apt),
					UpdatedAt:    time.Now(),
				}, nil
			},
		},
		pkg: &mockSysPackageInstaller{
			installFn: func(_ context.Context, res *resource.SystemPackageSet, name string) (*resource.SystemPackageSetState, error) {
				record("package:" + name)
				return &resource.SystemPackageSetState{
					InstallerRef:  res.SystemPackageSetSpec.InstallerRef,
					RepositoryRef: res.SystemPackageSetSpec.RepositoryRef,
					Packages:      res.SystemPackageSetSpec.Packages,
					UpdatedAt:     time.Now(),
				}, nil
			},
		},
	}
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
				Install: resource.CommandSpec{Command: name + " install -y"},
				Remove:  resource.CommandSpec{Command: name + " remove -y"},
				Check:   resource.CommandSpec{Command: "dpkg -s"},
			},
		},
	}
}

func testSystemPackageRepository(name, installerRef string) *resource.SystemPackageRepository {
	spec := &resource.SystemPackageRepositorySpec{InstallerRef: installerRef}
	// Discriminated-union contract: only the arm matching installerRef
	// is populated. Tests exercising future arms (dnf / apk / pacman per
	// #213) will need their own *Source assignment here when those arms
	// land; today only "apt" is wired so a non-apt installerRef yields a
	// spec with no source block (engine tests using "dnf" assert on
	// state-map counting and do not depend on the source).
	if installerRef == resource.InstallerRefApt {
		spec.Apt = &resource.AptSource{
			URL:        "https://example.com/" + name + "/repo",
			KeyURL:     "https://example.com/" + name + "/gpg",
			KeyHash:    "sha256:0000000000000000000000000000000000000000000000000000000000000000",
			Suite:      "stable",
			Components: []string{"main"},
		}
	}
	return &resource.SystemPackageRepository{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindSystemPackageRepository,
			Metadata:     resource.Metadata{Name: name},
		},
		SystemPackageRepositorySpec: spec,
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

// setupState locks the store, applies fn to a fresh SystemState, saves, and
// unlocks. The deferred unlock ensures the lock is released even if a require
// assertion fails mid-setup.
func setupState(t *testing.T, store *state.Store[state.SystemState], fn func(*state.SystemState)) {
	t.Helper()
	require.NoError(t, store.Lock())
	defer func() { _ = store.Unlock() }()
	st := state.NewSystemState()
	fn(st)
	require.NoError(t, store.Save(st))
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

	stateDir := t.TempDir()
	store, err := state.NewStore[state.SystemState](stateDir)
	require.NoError(t, err)

	rec := newCallRecorder()
	engine := NewSystemEngine(rec.installer, rec.repo, rec.pkg, store)
	engine.SetPrivilegeHandler(&mockPrivilegeHandler{})

	resources := []resource.Resource{
		testSystemInstaller("apt"),
		testSystemPackageRepository("docker", "apt"),
		testSystemPackageSet("docker-pkgs", "apt", "docker", []string{"docker-ce"}),
	}

	err = engine.Apply(context.Background(), resources)
	require.NoError(t, err)

	assert.Equal(t,
		[]string{"installer:apt", "repo:docker", "package:docker-pkgs"},
		rec.snapshot())
}

func TestSystemEngine_Apply_NoChanges(t *testing.T) {
	t.Parallel()
	s := newSystemEngineTestSetup(t)

	// Pre-populate state
	setupState(t, s.store, func(st *state.SystemState) {
		st.SystemInstallers["apt"] = &resource.SystemInstallerState{
			Version:   "1.0.0",
			UpdatedAt: time.Now(),
		}
		st.SystemPackageRepositories["docker"] = &resource.SystemPackageRepositoryState{
			InstallerRef: resource.InstallerRefApt,
			Apt: &resource.AptSource{
				URL:        "https://example.com/docker/repo",
				KeyURL:     "https://example.com/docker/gpg",
				KeyHash:    "sha256:0000000000000000000000000000000000000000000000000000000000000000",
				Suite:      "stable",
				Components: []string{"main"},
			},
			UpdatedAt: time.Now(),
		}
		st.SystemPackages["docker-pkgs"] = &resource.SystemPackageSetState{
			InstallerRef:  resource.InstallerRefApt,
			RepositoryRef: "docker",
			Packages:      []string{"docker-ce", "containerd.io"},
			UpdatedAt:     time.Now(),
		}
	})

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
	setupState(t, store, func(st *state.SystemState) {
		st.SystemInstallers["apt"] = &resource.SystemInstallerState{
			Version:   "1.0.0",
			UpdatedAt: time.Now(),
		}
		st.SystemPackageRepositories["docker"] = &resource.SystemPackageRepositoryState{
			InstallerRef: resource.InstallerRefApt,
			Apt:          &resource.AptSource{URL: "https://example.com/docker/repo"},
			UpdatedAt:    time.Now(),
		}
		st.SystemPackages["docker-pkgs"] = &resource.SystemPackageSetState{
			InstallerRef:  resource.InstallerRefApt,
			RepositoryRef: "docker",
			Packages:      []string{"docker-ce"},
			UpdatedAt:     time.Now(),
		}
	})

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
	st, err := store.Load()
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

	// Verify method is set to "system" for all emitted success-path events
	for _, e := range starts {
		assert.Equal(t, "system", e.Method, "system engine start events should have Method=\"system\"")
	}
	for _, e := range completes {
		assert.Equal(t, "system", e.Method, "system engine complete events should have Method=\"system\"")
	}
}

func TestSystemEngine_Apply_UpgradeRepo(t *testing.T) {
	t.Parallel()
	s := newSystemEngineTestSetup(t)

	// Pre-populate state with old URL
	setupState(t, s.store, func(st *state.SystemState) {
		st.SystemInstallers["apt"] = &resource.SystemInstallerState{
			Version:   "1.0.0",
			UpdatedAt: time.Now(),
		}
		st.SystemPackageRepositories["docker"] = &resource.SystemPackageRepositoryState{
			InstallerRef: resource.InstallerRefApt,
			Apt: &resource.AptSource{
				URL:        "https://old.example.com/docker/repo",
				KeyURL:     "https://example.com/docker/gpg",
				KeyHash:    "sha256:0000000000000000000000000000000000000000000000000000000000000000",
				Suite:      "stable",
				Components: []string{"main"},
			},
			UpdatedAt: time.Now(),
		}
	})

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
	require.NotNil(t, updatedSt.SystemPackageRepositories["docker"].Apt)
	assert.Equal(t, "https://example.com/docker/repo", updatedSt.SystemPackageRepositories["docker"].Apt.URL)
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
	setupState(t, s.store, func(st *state.SystemState) {
		st.SystemInstallers["apt"] = &resource.SystemInstallerState{
			Version:   "1.0.0",
			UpdatedAt: time.Now(),
		}
		st.SystemPackageRepositories["docker"] = &resource.SystemPackageRepositoryState{
			InstallerRef: resource.InstallerRefApt,
			Apt: &resource.AptSource{
				URL:        "https://example.com/docker/repo",
				KeyURL:     "https://example.com/docker/gpg",
				KeyHash:    "sha256:0000000000000000000000000000000000000000000000000000000000000000",
				Suite:      "stable",
				Components: []string{"main"},
			},
			UpdatedAt: time.Now(),
		}
		st.SystemPackages["docker-pkgs"] = &resource.SystemPackageSetState{
			InstallerRef:  resource.InstallerRefApt,
			RepositoryRef: "docker",
			Packages:      []string{"docker-ce"},
			UpdatedAt:     time.Now(),
		}
	})

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
	setupState(t, s.store, func(st *state.SystemState) {
		st.SystemInstallers["apt"] = &resource.SystemInstallerState{
			Version:   "1.0.0",
			UpdatedAt: time.Now(),
		}
		st.SystemPackageRepositories["docker"] = &resource.SystemPackageRepositoryState{
			InstallerRef: resource.InstallerRefApt,
			Apt:          &resource.AptSource{URL: "https://example.com/docker/repo"},
			UpdatedAt:    time.Now(),
		}
	})

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
	setupState(t, s.store, func(st *state.SystemState) {
		st.SystemInstallers["apt"] = &resource.SystemInstallerState{
			Version:   "1.0.0",
			UpdatedAt: time.Now(),
		}
		st.SystemPackageRepositories["docker"] = &resource.SystemPackageRepositoryState{
			InstallerRef: resource.InstallerRefApt,
			Apt:          &resource.AptSource{URL: "https://example.com/docker/repo"},
			UpdatedAt:    time.Now(),
		}
		st.SystemPackages["docker-pkgs"] = &resource.SystemPackageSetState{
			InstallerRef:  resource.InstallerRefApt,
			RepositoryRef: "docker",
			Packages:      []string{"docker-ce"},
			UpdatedAt:     time.Now(),
		}
	})

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
	setupState(t, store, func(st *state.SystemState) {
		st.SystemPackages["failing-pkg"] = &resource.SystemPackageSetState{
			InstallerRef: resource.InstallerRefApt,
			Packages:     []string{"a"},
			UpdatedAt:    time.Now(),
		}
	})

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
	st, err := store.Load()
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
	setupState(t, store, func(st *state.SystemState) {
		st.SystemPackages["good-pkg"] = &resource.SystemPackageSetState{
			InstallerRef: resource.InstallerRefApt,
			Packages:     []string{"a"},
			UpdatedAt:    time.Now(),
		}
		st.SystemPackageRepositories["bad-repo"] = &resource.SystemPackageRepositoryState{
			InstallerRef: resource.InstallerRefApt,
			Apt:          &resource.AptSource{URL: "https://example.com"},
			UpdatedAt:    time.Now(),
		}
	})

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
	st, err := store.Load()
	require.NoError(t, err)
	assert.Nil(t, st.SystemPackages["good-pkg"], "successful removal should be persisted even when later batch fails")
	assert.NotNil(t, st.SystemPackageRepositories["bad-repo"], "failed removal should leave state intact")
}

// Given:  one installer ("apt") + three SystemPackageRepository ({alpha,mid,zeta}-repo)
//   - three SystemPackageSet bound to each.
//
// When:   engine.Apply runs once for each of the 6 permutations (3!) of the input slice.
// Then:   per-resource install calls are emitted in three layers (installer -> repo -> package),
//
//	and within each multi-node layer the calls are ascending by name — the recorded
//	allCalls slice is byte-identical across all 6 permutations.
//
// Guards (engine-determinism contract):
//   - cross-layer order: installer before repos before package sets
//   - intra-layer order: alphabetical by name (for log / state / rollback predictability)
//
// Does NOT guard:
//   - APT precedence (governed by Pin-Priority; APT scans /etc/apt/sources.list.d/ in
//     C-collation order independent of write order)
//   - filesystem sequencing inside PackageRepositoryInstaller.Install (covered by apt-installer tests)
//   - apt-get update timing; dpkg-frontend lock (taken only by the apt-get update step,
//     NOT by keyring / sources.list file writes — the latter would race under parallelism,
//     but the engine is sequential today, so this is not a regression risk)
//   - parallel intra-layer apply (not a current code path)
//   - mid-layer abort / partial-layer rollback semantics (covered by the sibling
//     TestSystemEngine_Apply_PartialLayerFailure_SkipsDependents below and by
//     TestSystemEngine_Apply_ErrorMidLayer above)
//   - removal-path ordering / Apply -> handleSystemRemovals — out of scope
func TestSystemEngine_Apply_IntraLayerAlphabeticalOrder(t *testing.T) {
	t.Parallel()

	wantAlphabetical := []string{
		"installer:apt",
		"repo:alpha-repo", "repo:mid-repo", "repo:zeta-repo",
		"package:alpha-pkgs", "package:mid-pkgs", "package:zeta-pkgs",
	}

	// All 6 permutations of the three repo+package pairs.
	// Each pair (repo + matching package) moves together since the package
	// depends on its repo; we only permute the pair order in the input slice.
	pairOrders := [][]string{
		{"alpha", "mid", "zeta"},
		{"alpha", "zeta", "mid"},
		{"mid", "alpha", "zeta"},
		{"mid", "zeta", "alpha"},
		{"zeta", "alpha", "mid"},
		{"zeta", "mid", "alpha"},
	}

	for _, perm := range pairOrders {
		t.Run("input="+perm[0]+","+perm[1]+","+perm[2], func(t *testing.T) {
			t.Parallel()

			stateDir := t.TempDir()
			store, err := state.NewStore[state.SystemState](stateDir)
			require.NoError(t, err)

			rec := newCallRecorder()
			engine := NewSystemEngine(rec.installer, rec.repo, rec.pkg, store)
			engine.SetPrivilegeHandler(&mockPrivilegeHandler{})

			resources := []resource.Resource{testSystemInstaller("apt")}
			for _, p := range perm {
				resources = append(resources,
					testSystemPackageRepository(p+"-repo", "apt"),
					testSystemPackageSet(p+"-pkgs", "apt", p+"-repo", []string{p + "-binary"}),
				)
			}

			err = engine.Apply(context.Background(), resources)
			require.NoError(t, err)

			gotCalls := rec.snapshot()
			assert.Equalf(t, wantAlphabetical, gotCalls,
				"apply order mismatch (input perm=%v); entries are '<kind>:<name>'; "+
					"contract: layers installer->repo->package, intra-layer ascending by name",
				perm)

			// Structural invariants — survive future slice-length changes.
			// indexOf wraps slices.Index with require.Contains so a missing
			// entry surfaces as a clear "<entry> missing from call log" failure
			// rather than letting slices.Index return -1 and producing a
			// false-pass on a downstream assert.Less(-1, ...) comparison.
			indexOf := func(entry string) int {
				require.Containsf(t, gotCalls, entry, "call log missing required entry %q", entry)
				return slices.Index(gotCalls, entry)
			}
			assert.Less(t, indexOf("repo:alpha-repo"), indexOf("repo:mid-repo"))
			assert.Less(t, indexOf("repo:mid-repo"), indexOf("repo:zeta-repo"))
			assert.Less(t, indexOf("repo:zeta-repo"), indexOf("package:alpha-pkgs"),
				"intra-layer boundary: every repo must precede every package")
		})
	}
}

// TestSystemEngine_Apply_PartialLayerFailure_SkipsDependents covers the case
// where one repo in a same-layer pair fails. The single-repo
// TestSystemEngine_Apply_ErrorMidLayer cannot exercise this because it has no
// sibling at the failing layer.
//
// Engine failure semantics (system_engine.go layer loop): Apply iterates
// layer.Nodes sequentially in sortNodesByKind order (alphabetical for System
// kinds); the first error returns from the layer loop, all remaining nodes in
// the same layer are skipped, and all subsequent layers are skipped.
//
// The contract this test guards is engine-level: a layer error skips the
// dependent package layer wholesale. The downstream APT correctness property
// ("no apt-get install runs against a missing keyring") is a property of
// internal/installer/apt/ tests, not this test.
func TestSystemEngine_Apply_PartialLayerFailure_SkipsDependents(t *testing.T) {
	t.Parallel()

	// Known fixture repo names; any other name reaching the repoMock means a
	// future edit added a third repo without extending this test — fail loud.
	knownRepos := map[string]struct{}{"alpha-repo": {}, "zeta-repo": {}}

	cases := []struct {
		failingRepo string
		want        []string
		// alphaPersisted: true => fails=zeta-repo (alpha already succeeded),
		//                  false => fails=alpha-repo (alpha was the failure).
		alphaPersisted bool
	}{
		{
			failingRepo:    "alpha-repo",
			want:           []string{"installer:apt", "repo:alpha-repo"},
			alphaPersisted: false,
		},
		{
			failingRepo:    "zeta-repo",
			want:           []string{"installer:apt", "repo:alpha-repo", "repo:zeta-repo"},
			alphaPersisted: true,
		},
	}

	for _, tc := range cases {
		t.Run("fails="+tc.failingRepo, func(t *testing.T) {
			t.Parallel()

			stateDir := t.TempDir()
			store, err := state.NewStore[state.SystemState](stateDir)
			require.NoError(t, err)

			rec := newCallRecorder()

			// Override rec.repo to inject the per-name failure. The default
			// installFn from newCallRecorder is replaced wholesale so the
			// failure Given is visible at the call site.
			rec.repo.installFn = func(_ context.Context, res *resource.SystemPackageRepository, name string) (*resource.SystemPackageRepositoryState, error) {
				if _, ok := knownRepos[name]; !ok {
					// Fail loud: defeats silent typo-degradation if a future
					// edit adds a repo to the fixture without extending the
					// failure-injection table.
					t.Errorf("repoMock saw unexpected repo %q; extend knownRepos / cases", name)
				}
				rec.record("repo:" + name)
				if name == tc.failingRepo {
					// --- Mock error rationale ---
					// Mock error mirrors a real PackageRepositoryInstaller failure:
					//   - "verify key" wrap from apt/repository.go (verify-key step)
					//   - "checksum mismatch" substring from internal/checksum/checksum.go
					// This is the no-side-effect failure mode (hash verification refuses
					// to write the keyring). The engine treats any non-nil error
					// identically, so the exact text is illustrative, not part of the
					// engine contract.
					return nil, fmt.Errorf("apt: repository %q: verify key: checksum mismatch: expected sha256:aaa, got sha256:bbb", name)
				}
				return &resource.SystemPackageRepositoryState{
					InstallerRef: res.SystemPackageRepositorySpec.InstallerRef,
					Apt:          cloneAptSource(res.SystemPackageRepositorySpec.Apt),
					UpdatedAt:    time.Now(),
				}, nil
			}

			// Override rec.pkg to fail loud if the package layer is ever
			// entered. This is a security canary: the layer-error contract
			// requires the package layer to be skipped wholesale on any
			// repo-layer error. A non-nil error here causes engine.Apply to
			// propagate it, but the assert.Equal on rec.snapshot() catches the
			// "package:*" entry first and gives a clearer diagnostic.
			rec.pkg.installFn = func(_ context.Context, _ *resource.SystemPackageSet, name string) (*resource.SystemPackageSetState, error) {
				rec.record("package:" + name)
				return nil, fmt.Errorf("package layer must not be entered after repo-layer abort; got package:%s", name)
			}

			engine := NewSystemEngine(rec.installer, rec.repo, rec.pkg, store)
			engine.SetPrivilegeHandler(&mockPrivilegeHandler{})

			resources := []resource.Resource{
				testSystemInstaller("apt"),
				testSystemPackageRepository("alpha-repo", "apt"),
				testSystemPackageRepository("zeta-repo", "apt"),
				testSystemPackageSet("alpha-pkgs", "apt", "alpha-repo", []string{"alpha-binary"}),
				testSystemPackageSet("zeta-pkgs", "apt", "zeta-repo", []string{"zeta-binary"}),
			}

			err = engine.Apply(context.Background(), resources)

			// BDD assertion order: did it fail -> which -> why -> what ran -> what persisted.
			require.Error(t, err)                         // (1) failure occurred
			require.ErrorContains(t, err, tc.failingRepo) // (2) identification
			require.ErrorContains(t, err, "verify key")   // (3) failure class
			gotCalls := rec.snapshot()
			assert.Equal(t, tc.want, gotCalls) // (4) observed calls (Equal also asserts length and absence of package:* entries)
			// Anti-typo guard: the failing repo MUST have been observed; defeats
			// silent degradation if tc.failingRepo gets out of sync with the fixture.
			assert.Contains(t, gotCalls, "repo:"+tc.failingRepo,
				"failing repo never reached repoMock; failure injection is unreachable")

			// --- State after partial-layer failure ---
			// (mirrors TestSystemEngine_Apply_ErrorMidLayer state-store pattern)
			require.NoError(t, store.Lock())
			defer func() { _ = store.Unlock() }()
			st, err := store.Load()
			require.NoError(t, err)

			assert.NotNil(t, st.SystemInstallers["apt"], "prior installer layer must be persisted")
			assert.Nil(t, st.SystemPackageRepositories[tc.failingRepo], "failed repo must not be persisted")
			assert.Nil(t, st.SystemPackages["alpha-pkgs"], "package layer was never entered")
			assert.Nil(t, st.SystemPackages["zeta-pkgs"], "package layer was never entered")

			if tc.alphaPersisted {
				// --- Non-transactional semantics ---
				// IMPORTANT: Apply is non-transactional. When a sibling repo in a
				// layer fails, any previously-successful sibling's side effects
				// are NOT rolled back:
				//   - in this test: state-store record for alpha-repo persists
				//     via flushAndReturn calling stateCache.Flush() before
				//     returning on error
				//   - in production: keyring files at /usr/share/keyrings/<name>.gpg
				//     and source list entries at /etc/apt/sources.list.d/<name>.list
				//     also remain on disk
				//   - the failing node's OWN partial writes (if any) are likewise
				//     not rolled back; this test asserts state-store absence for
				//     the failing node, NOT filesystem absence — the mock returns
				//     (nil, error) before any persistence call
				//
				// SECURITY IMPLICATION: a non-nil error from Apply does NOT imply
				// an unchanged system. Trust-store entries (APT keyrings, sources
				// lists) for earlier-succeeded siblings remain on disk. An
				// operator interpreting "apply failed" as "no changes" may miss
				// that a third-party APT source was added.
				//
				// This is intentional engine semantics — rollback is the
				// operator's responsibility. If a future change introduces
				// rollback-on-error, the following assertion is the regression
				// canary.
				assert.NotNil(t, st.SystemPackageRepositories["alpha-repo"],
					"partial-layer persistence canary: alpha-repo succeeded before zeta-repo failed; must remain in state")
			} else {
				assert.Nil(t, st.SystemPackageRepositories["alpha-repo"],
					"alpha-repo failed first; zeta-repo never attempted; neither persisted")
				assert.Nil(t, st.SystemPackageRepositories["zeta-repo"])
			}
		})
	}
}
