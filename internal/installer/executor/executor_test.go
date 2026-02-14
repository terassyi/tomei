package executor

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/installer/reconciler"
	"github.com/terassyi/tomei/internal/resource"
)

// mockInstaller is a mock implementation for testing.
type mockInstaller struct {
	installFunc func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error)
	removeFunc  func(ctx context.Context, st *resource.ToolState, name string) error
}

func (m *mockInstaller) Install(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
	if m.installFunc != nil {
		return m.installFunc(ctx, res, name)
	}
	return &resource.ToolState{
		InstallerRef: res.ToolSpec.InstallerRef,
		Version:      res.ToolSpec.Version,
		InstallPath:  "/tools/" + name,
		BinPath:      "/bin/" + name,
	}, nil
}

func (m *mockInstaller) Remove(ctx context.Context, st *resource.ToolState, name string) error {
	if m.removeFunc != nil {
		return m.removeFunc(ctx, st, name)
	}
	return nil
}

// mockStateStore is a mock implementation for testing.
type mockStateStore struct {
	data       map[string]*resource.ToolState
	saveFunc   func(name string, state *resource.ToolState) error
	deleteFunc func(name string) error
}

func newMockStateStore() *mockStateStore {
	return &mockStateStore{
		data: make(map[string]*resource.ToolState),
	}
}

func (s *mockStateStore) Load(name string) (*resource.ToolState, bool, error) {
	st, ok := s.data[name]
	return st, ok, nil
}

func (s *mockStateStore) Save(name string, state *resource.ToolState) error {
	if s.saveFunc != nil {
		return s.saveFunc(name, state)
	}
	s.data[name] = state
	return nil
}

func (s *mockStateStore) Delete(name string) error {
	if s.deleteFunc != nil {
		return s.deleteFunc(name)
	}
	delete(s.data, name)
	return nil
}

func TestNew(t *testing.T) {
	t.Parallel()
	mock := &mockInstaller{}
	store := newMockStateStore()

	exec := New(resource.KindTool, mock, store)
	assert.NotNil(t, exec)
}

func TestExecutor_Execute_Install(t *testing.T) {
	t.Parallel()
	mock := &mockInstaller{
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			return &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				InstallPath:  "/tools/" + name + "/" + res.ToolSpec.Version,
				BinPath:      "/bin/" + name,
			}, nil
		},
	}
	store := newMockStateStore()

	exec := New(resource.KindTool, mock, store)

	action := reconciler.Action[*resource.Tool, *resource.ToolState]{
		Type: resource.ActionInstall,
		Name: "ripgrep",
		Resource: &resource.Tool{
			ToolSpec: &resource.ToolSpec{
				InstallerRef: "download",
				Version:      "14.1.1",
			},
		},
	}

	err := exec.Execute(context.Background(), action)
	require.NoError(t, err)

	// Verify state was saved
	assert.Contains(t, store.data, "ripgrep")
	assert.Equal(t, "14.1.1", store.data["ripgrep"].Version)
}

func TestExecutor_Execute_Upgrade(t *testing.T) {
	t.Parallel()
	mock := &mockInstaller{
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			return &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				InstallPath:  "/tools/" + name + "/" + res.ToolSpec.Version,
				BinPath:      "/bin/" + name,
			}, nil
		},
	}
	store := newMockStateStore()
	store.data["ripgrep"] = &resource.ToolState{
		InstallerRef: "download",
		Version:      "14.0.0",
		InstallPath:  "/tools/ripgrep/14.0.0",
		BinPath:      "/bin/rg",
	}

	exec := New(resource.KindTool, mock, store)

	action := reconciler.Action[*resource.Tool, *resource.ToolState]{
		Type: resource.ActionUpgrade,
		Name: "ripgrep",
		Resource: &resource.Tool{
			ToolSpec: &resource.ToolSpec{
				InstallerRef: "download",
				Version:      "14.1.1",
			},
		},
	}

	err := exec.Execute(context.Background(), action)
	require.NoError(t, err)

	// Verify state was updated
	assert.Equal(t, "14.1.1", store.data["ripgrep"].Version)
}

func TestExecutor_Execute_Remove(t *testing.T) {
	t.Parallel()
	removed := false
	mock := &mockInstaller{
		removeFunc: func(ctx context.Context, st *resource.ToolState, name string) error {
			removed = true
			return nil
		},
	}
	store := newMockStateStore()
	store.data["ripgrep"] = &resource.ToolState{
		InstallerRef: "download",
		Version:      "14.1.1",
		InstallPath:  "/tools/ripgrep/14.1.1",
		BinPath:      "/bin/rg",
	}

	exec := New(resource.KindTool, mock, store)

	action := reconciler.Action[*resource.Tool, *resource.ToolState]{
		Type:  resource.ActionRemove,
		Name:  "ripgrep",
		State: store.data["ripgrep"],
	}

	err := exec.Execute(context.Background(), action)
	require.NoError(t, err)

	assert.True(t, removed)
	assert.NotContains(t, store.data, "ripgrep")
}

func TestExecutor_Execute_InstallError(t *testing.T) {
	t.Parallel()
	mock := &mockInstaller{
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			return nil, errors.New("download failed")
		},
	}
	store := newMockStateStore()

	exec := New(resource.KindTool, mock, store)

	action := reconciler.Action[*resource.Tool, *resource.ToolState]{
		Type: resource.ActionInstall,
		Name: "ripgrep",
		Resource: &resource.Tool{
			ToolSpec: &resource.ToolSpec{
				InstallerRef: "download",
				Version:      "14.1.1",
			},
		},
	}

	err := exec.Execute(context.Background(), action)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "download failed")
}

func TestExecutor_Execute_UnsupportedActionType(t *testing.T) {
	t.Parallel()
	mock := &mockInstaller{}
	store := newMockStateStore()

	exec := New(resource.KindTool, mock, store)

	action := reconciler.Action[*resource.Tool, *resource.ToolState]{
		Type: resource.ActionType("unknown"),
		Name: "ripgrep",
	}

	err := exec.Execute(context.Background(), action)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}
