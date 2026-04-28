package executor

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/state"
)

// newSystemStateCache creates a StateCache[SystemState] with Lock acquired and Init called.
func newSystemStateCache(t *testing.T) *StateCache[state.SystemState] {
	t.Helper()
	dir := t.TempDir()
	store, err := state.NewStore[state.SystemState](dir)
	require.NoError(t, err)
	require.NoError(t, store.Lock())
	t.Cleanup(func() { _ = store.Unlock() })
	sc := NewStateCache[state.SystemState](store)
	sc.Init(state.NewSystemState())
	return sc
}

// --- SystemInstaller Store ---

func TestSystemInstallerStore_SaveAndLoad(t *testing.T) {
	t.Parallel()
	sc := newSystemStateCache(t)
	sis := NewSystemInstallerStore(sc)

	st := &resource.SystemInstallerState{
		Version:   "2.4.12",
		UpdatedAt: time.Now(),
	}

	require.NoError(t, sis.Save("apt", st))

	loaded, exists, err := sis.Load("apt")
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, "2.4.12", loaded.Version)
}

func TestSystemInstallerStore_Delete(t *testing.T) {
	t.Parallel()
	sc := newSystemStateCache(t)
	sis := NewSystemInstallerStore(sc)

	require.NoError(t, sis.Save("apt", &resource.SystemInstallerState{Version: "2.4.12"}))
	require.NoError(t, sis.Delete("apt"))

	_, exists, err := sis.Load("apt")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestSystemInstallerStore_LoadNotFound(t *testing.T) {
	t.Parallel()
	sc := newSystemStateCache(t)
	sis := NewSystemInstallerStore(sc)

	_, exists, err := sis.Load("nonexistent")
	require.NoError(t, err)
	assert.False(t, exists)
}

// --- SystemPackageRepository Store ---

func TestSystemPackageRepositoryStore_SaveAndLoad(t *testing.T) {
	t.Parallel()
	sc := newSystemStateCache(t)
	srs := NewSystemPackageRepositoryStore(sc)

	st := &resource.SystemPackageRepositoryState{
		InstallerRef: "apt",
		Source: resource.SourceConfig{
			URL:    "https://download.docker.com/linux/ubuntu",
			KeyURL: "https://download.docker.com/linux/ubuntu/gpg",
		},
		InstalledFiles: []string{"/usr/share/keyrings/docker.gpg"},
		UpdatedAt:      time.Now(),
	}

	require.NoError(t, srs.Save("docker", st))

	loaded, exists, err := srs.Load("docker")
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, "apt", loaded.InstallerRef)
	assert.Equal(t, "https://download.docker.com/linux/ubuntu", loaded.Source.URL)
}

func TestSystemPackageRepositoryStore_Delete(t *testing.T) {
	t.Parallel()
	sc := newSystemStateCache(t)
	srs := NewSystemPackageRepositoryStore(sc)

	require.NoError(t, srs.Save("docker", &resource.SystemPackageRepositoryState{InstallerRef: "apt"}))
	require.NoError(t, srs.Delete("docker"))

	_, exists, err := srs.Load("docker")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestSystemPackageRepositoryStore_LoadNotFound(t *testing.T) {
	t.Parallel()
	sc := newSystemStateCache(t)
	srs := NewSystemPackageRepositoryStore(sc)

	_, exists, err := srs.Load("nonexistent")
	require.NoError(t, err)
	assert.False(t, exists)
}

// --- SystemPackageSet Store ---

func TestSystemPackageSetStore_SaveAndLoad(t *testing.T) {
	t.Parallel()
	sc := newSystemStateCache(t)
	sps := NewSystemPackageSetStore(sc)

	st := &resource.SystemPackageSetState{
		InstallerRef:      "apt",
		RepositoryRef:     "docker",
		Packages:          []string{"docker-ce", "docker-ce-cli"},
		InstalledVersions: map[string]string{"docker-ce": "24.0.7", "docker-ce-cli": "24.0.7"},
		UpdatedAt:         time.Now(),
	}

	require.NoError(t, sps.Save("docker-packages", st))

	loaded, exists, err := sps.Load("docker-packages")
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, "apt", loaded.InstallerRef)
	assert.Equal(t, []string{"docker-ce", "docker-ce-cli"}, loaded.Packages)
	assert.Equal(t, "24.0.7", loaded.InstalledVersions["docker-ce"])
}

func TestSystemPackageSetStore_Delete(t *testing.T) {
	t.Parallel()
	sc := newSystemStateCache(t)
	sps := NewSystemPackageSetStore(sc)

	require.NoError(t, sps.Save("docker-packages", &resource.SystemPackageSetState{
		InstallerRef: "apt",
		Packages:     []string{"docker-ce"},
	}))
	require.NoError(t, sps.Delete("docker-packages"))

	_, exists, err := sps.Load("docker-packages")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestSystemPackageSetStore_LoadNotFound(t *testing.T) {
	t.Parallel()
	sc := newSystemStateCache(t)
	sps := NewSystemPackageSetStore(sc)

	_, exists, err := sps.Load("nonexistent")
	require.NoError(t, err)
	assert.False(t, exists)
}

// --- Concurrent Access ---

func TestSystemStores_ConcurrentMixed(t *testing.T) {
	t.Parallel()
	sc := newSystemStateCache(t)
	sis := NewSystemInstallerStore(sc)
	srs := NewSystemPackageRepositoryStore(sc)
	sps := NewSystemPackageSetStore(sc)

	const n = 10
	var wg sync.WaitGroup

	// Concurrent writes across all three system store types
	for i := range n {
		wg.Go(func() {
			name := fmt.Sprintf("installer-%d", i)
			_ = sis.Save(name, &resource.SystemInstallerState{Version: fmt.Sprintf("2.%d.0", i)})
		})
		wg.Go(func() {
			name := fmt.Sprintf("repo-%d", i)
			_ = srs.Save(name, &resource.SystemPackageRepositoryState{InstallerRef: "apt"})
		})
		wg.Go(func() {
			name := fmt.Sprintf("pkgset-%d", i)
			_ = sps.Save(name, &resource.SystemPackageSetState{
				InstallerRef: "apt",
				Packages:     []string{fmt.Sprintf("pkg-%d", i)},
			})
		})
	}
	wg.Wait()

	// Verify all entries are present
	for i := range n {
		loaded, exists, err := sis.Load(fmt.Sprintf("installer-%d", i))
		require.NoError(t, err)
		assert.True(t, exists)
		assert.Equal(t, fmt.Sprintf("2.%d.0", i), loaded.Version)

		_, exists, err = srs.Load(fmt.Sprintf("repo-%d", i))
		require.NoError(t, err)
		assert.True(t, exists)

		_, exists, err = sps.Load(fmt.Sprintf("pkgset-%d", i))
		require.NoError(t, err)
		assert.True(t, exists)
	}
}
