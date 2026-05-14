package main

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/installer/apt"
	"github.com/terassyi/tomei/internal/installer/command"
	"github.com/terassyi/tomei/internal/installer/download"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/system"
)

func TestValidatorInstaller_Remove_IsNoOp(t *testing.T) {
	t.Parallel()
	// Remove is a no-op: system package managers are OS-managed, so
	// "removing" just cleans the state entry.
	a := &validatorInstaller{validator: nil}
	err := a.Remove(context.Background(), nil, "apt")
	require.NoError(t, err)
}

func TestValidatorInstaller_Install_NilValidator(t *testing.T) {
	t.Parallel()
	// When distro detection fails, validator is nil.
	// Install should return a clear error instead of panicking.
	a := &validatorInstaller{validator: nil}
	_, err := a.Install(context.Background(), nil, "apt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "package manager validation unavailable")
}

func TestUnsupportedHostRepoInstaller_Install(t *testing.T) {
	t.Parallel()
	installer := &unsupportedHostRepoInstaller{}

	_, err := installer.Install(context.Background(), nil, "docker-repo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a supported Linux package manager")
	assert.Contains(t, err.Error(), "docker-repo")
}

func TestUnsupportedHostRepoInstaller_Install_QuotesControlChars(t *testing.T) {
	t.Parallel()
	// %q Go-quotes control characters in the name, defanging log-injection
	// via crafted manifest names. Verify the literal \n (two chars) appears
	// in the error string rather than a raw newline.
	installer := &unsupportedHostRepoInstaller{}

	_, err := installer.Install(context.Background(), nil, "repo\nINJECT")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `\n`, "name with newline must be Go-quoted in error message")
	assert.NotContains(t, err.Error(), "repo\nINJECT", "raw newline must not appear in error message")
}

func TestUnsupportedHostRepoInstaller_Remove(t *testing.T) {
	// Cannot t.Parallel(): slog.SetDefault mutates global state.
	// Subtests below run sequentially for the same reason — DO NOT add
	// t.Parallel() inside the subtests; capture-then-cleanup breaks because
	// parallel siblings would race on the same process-global default
	// logger.
	installer := &unsupportedHostRepoInstaller{}

	capture := func(t *testing.T) *bytes.Buffer {
		t.Helper()
		// Pattern mirrors internal/state/store_test.go.
		var buf bytes.Buffer
		handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
		oldLogger := slog.Default()
		slog.SetDefault(slog.New(handler))
		t.Cleanup(func() { slog.SetDefault(oldLogger) })
		return &buf
	}

	t.Run("nil state returns nil and warns", func(t *testing.T) {
		buf := capture(t)
		err := installer.Remove(context.Background(), nil, "docker-repo")
		require.NoError(t, err, "Remove must return nil so stale state can be cleaned up")
		assert.Contains(t, buf.String(), "name=docker-repo", "warn log must include the repo name attribute for cross-host visibility")
		assert.Contains(t, buf.String(), "package repository", "warn log must mention the resource kind")
	})

	t.Run("non-nil state surfaces orphaned files", func(t *testing.T) {
		buf := capture(t)
		st := &resource.SystemPackageRepositoryState{
			InstalledFiles: []string{"/usr/share/keyrings/docker.gpg", "/etc/apt/sources.list.d/docker.list"},
		}
		err := installer.Remove(context.Background(), st, "docker-repo")
		require.NoError(t, err)
		// Forensic visibility: which actual files survive on the originating host.
		assert.Contains(t, buf.String(), "/usr/share/keyrings/docker.gpg",
			"warn log must include InstalledFiles so dotfile-sync users can audit the originating host")
	})

	t.Run("name with control chars is escaped in warn log", func(t *testing.T) {
		buf := capture(t)
		err := installer.Remove(context.Background(), nil, "repo\nINJECT")
		require.NoError(t, err)
		// slog.NewTextHandler quotes string attributes that contain control
		// chars (logfmt-style strconv-quoted), so a raw newline does not
		// land in the log stream.
		assert.NotContains(t, buf.String(), "\nINJECT",
			"control chars in name must not produce a raw newline in the warn log")
	})
}

func TestSkipPackageInstaller(t *testing.T) {
	t.Parallel()
	installer := &skipPackageInstaller{}

	t.Run("Install", func(t *testing.T) {
		t.Parallel()
		_, err := installer.Install(context.Background(), nil, "dev-tools")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet implemented")
		assert.Contains(t, err.Error(), "system: package")
		assert.Contains(t, err.Error(), "dev-tools")
	})

	t.Run("Remove", func(t *testing.T) {
		t.Parallel()
		err := installer.Remove(context.Background(), nil, "dev-tools")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet implemented")
		assert.Contains(t, err.Error(), "system: package")
	})
}

func TestSelectRepoInstaller(t *testing.T) {
	t.Parallel()
	aptClient := apt.New(command.NewExecutor(""))
	downloader := download.NewDownloader()

	// aptValidator and dnfValidator are constructed via system.NewValidator
	// so SupportedInstallers() returns a real list backed by the distro
	// family map (debian → [apt], fedora → [dnf]).
	aptValidator, err := system.NewValidator(&system.DistroInfo{ID: "debian"}, nil)
	require.NoError(t, err)
	dnfValidator, err := system.NewValidator(&system.DistroInfo{ID: "fedora"}, nil)
	require.NoError(t, err)

	t.Run("validator nil returns placeholder", func(t *testing.T) {
		t.Parallel()
		got := selectRepoInstaller(nil, aptClient, downloader)
		_, ok := got.(*unsupportedHostRepoInstaller)
		assert.True(t, ok, "nil validator must return *unsupportedHostRepoInstaller, got %T", got)
	})

	t.Run("apt-supporting distro returns non-placeholder", func(t *testing.T) {
		t.Parallel()
		got := selectRepoInstaller(aptValidator, aptClient, downloader)
		require.NotNil(t, got)
		// Negative assertion (rather than positive type-assert against
		// *apt.PackageRepositoryInstaller) keeps the test resilient to a
		// future apt type rename. We only care that the placeholder is
		// NOT returned on an apt host.
		_, isPlaceholder := got.(*unsupportedHostRepoInstaller)
		assert.False(t, isPlaceholder, "apt-supporting distro must not return placeholder, got %T", got)
	})

	t.Run("non-apt distro returns placeholder", func(t *testing.T) {
		t.Parallel()
		// Fedora supports DNF, not APT. Distro detection succeeds, so the
		// validator is non-nil — but the host has no APT support. Without
		// this gate the apt installer would run on Fedora and surface raw
		// apt/gpg errors instead of the documented platform-availability
		// error.
		got := selectRepoInstaller(dnfValidator, aptClient, downloader)
		_, ok := got.(*unsupportedHostRepoInstaller)
		assert.True(t, ok, "non-apt distro must return placeholder, got %T", got)
	})

	t.Run("nil aptClient returns placeholder (defense-in-depth)", func(t *testing.T) {
		t.Parallel()
		got := selectRepoInstaller(aptValidator, nil, downloader)
		_, ok := got.(*unsupportedHostRepoInstaller)
		assert.True(t, ok, "nil aptClient must return placeholder, got %T", got)
	})

	t.Run("nil downloader returns placeholder (defense-in-depth)", func(t *testing.T) {
		t.Parallel()
		got := selectRepoInstaller(aptValidator, aptClient, nil)
		_, ok := got.(*unsupportedHostRepoInstaller)
		assert.True(t, ok, "nil downloader must return placeholder, got %T", got)
	})
}

func TestCreateSystemEngine_NilDownloader(t *testing.T) {
	t.Parallel()
	// Defense-in-depth: the sole production caller (apply.go) always
	// constructs a real downloader, but a nil here would surface as a panic
	// deep inside Install. Fail fast with a clear error instead.
	_, err := createSystemEngine(t.TempDir(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "downloader is required")
}

func TestCreateSystemEngine_HappyPath(t *testing.T) {
	t.Parallel()
	// Smoke test: catches future signature drift in state.NewStore or
	// engine.NewSystemEngine. Distro detection may or may not succeed
	// depending on the host running the test — both branches produce a
	// non-nil engine (selectRepoInstaller falls back to the placeholder
	// when validator is nil), so this test is host-agnostic.
	eng, err := createSystemEngine(t.TempDir(), download.NewDownloader())
	require.NoError(t, err)
	assert.NotNil(t, eng)
}

func TestFilterSupportedSystemResources(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		supported, skipped := filterSupportedSystemResources(nil)
		assert.Empty(t, supported)
		assert.Empty(t, skipped)
	})

	t.Run("installer only", func(t *testing.T) {
		t.Parallel()
		resources := []resource.Resource{
			&resource.SystemInstaller{BaseResource: resource.BaseResource{ResourceKind: resource.KindSystemInstaller, Metadata: resource.Metadata{Name: "apt"}}},
		}
		supported, skipped := filterSupportedSystemResources(resources)
		assert.Len(t, supported, 1)
		assert.Empty(t, skipped)
	})

	t.Run("repo only", func(t *testing.T) {
		t.Parallel()
		resources := []resource.Resource{
			&resource.SystemPackageRepository{BaseResource: resource.BaseResource{ResourceKind: resource.KindSystemPackageRepository, Metadata: resource.Metadata{Name: "docker-repo"}}},
		}
		supported, skipped := filterSupportedSystemResources(resources)
		require.Len(t, supported, 1)
		assert.Equal(t, "docker-repo", supported[0].Name())
		assert.Empty(t, skipped)
	})

	t.Run("packageset only", func(t *testing.T) {
		t.Parallel()
		resources := []resource.Resource{
			&resource.SystemPackageSet{BaseResource: resource.BaseResource{ResourceKind: resource.KindSystemPackageSet, Metadata: resource.Metadata{Name: "dev-tools"}}},
		}
		supported, skipped := filterSupportedSystemResources(resources)
		assert.Empty(t, supported)
		require.Len(t, skipped, 1)
		assert.Equal(t, "dev-tools", skipped[0].Name())
	})

	t.Run("mixed", func(t *testing.T) {
		t.Parallel()
		resources := []resource.Resource{
			&resource.SystemInstaller{BaseResource: resource.BaseResource{ResourceKind: resource.KindSystemInstaller, Metadata: resource.Metadata{Name: "apt"}}},
			&resource.SystemPackageRepository{BaseResource: resource.BaseResource{ResourceKind: resource.KindSystemPackageRepository, Metadata: resource.Metadata{Name: "docker-repo"}}},
			&resource.SystemPackageSet{BaseResource: resource.BaseResource{ResourceKind: resource.KindSystemPackageSet, Metadata: resource.Metadata{Name: "dev-tools"}}},
		}
		supported, skipped := filterSupportedSystemResources(resources)
		require.Len(t, supported, 2)
		// Order matches input order — append-based filter is deterministic.
		assert.Equal(t, "apt", supported[0].Name())
		assert.Equal(t, "docker-repo", supported[1].Name())
		require.Len(t, skipped, 1)
		assert.Equal(t, "dev-tools", skipped[0].Name())
	})
}
