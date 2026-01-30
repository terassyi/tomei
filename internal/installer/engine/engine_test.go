package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/toto/internal/resource"
	"github.com/terassyi/toto/internal/state"
)

// mockToolInstaller is a mock implementation for testing.
type mockToolInstaller struct {
	installFunc func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error)
	removeFunc  func(ctx context.Context, st *resource.ToolState, name string) error
}

func (m *mockToolInstaller) Install(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
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

func (m *mockToolInstaller) Remove(ctx context.Context, st *resource.ToolState, name string) error {
	if m.removeFunc != nil {
		return m.removeFunc(ctx, st, name)
	}
	return nil
}

func TestNewEngine(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := state.NewStore[state.UserState](tmpDir)
	require.NoError(t, err)

	mock := &mockToolInstaller{}
	engine := NewEngine(mock, store)

	assert.NotNil(t, engine)
}

func TestEngine_Apply(t *testing.T) {
	// Create test config directory with CUE file
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "tools.cue")
	cueContent := `package toto

tool: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "test-tool"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/test-tool.tar.gz"
			checksum: {
				value: "sha256:abc123"
			}
			archiveType: "tar.gz"
		}
	}
}
`
	err := os.WriteFile(cueFile, []byte(cueContent), 0644)
	require.NoError(t, err)

	// Setup mock and store
	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	installedTools := make(map[string]*resource.ToolState)
	mock := &mockToolInstaller{
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			st := &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				InstallPath:  "/tools/" + name + "/" + res.ToolSpec.Version,
				BinPath:      "/bin/" + name,
			}
			installedTools[name] = st
			return st, nil
		},
	}

	engine := NewEngine(mock, store)

	// Run Apply
	err = engine.Apply(context.Background(), configDir)
	require.NoError(t, err)

	// Verify tool was installed
	assert.Contains(t, installedTools, "test-tool")
	assert.Equal(t, "1.0.0", installedTools["test-tool"].Version)

	// Verify state was updated
	require.NoError(t, store.Lock())
	defer func() { _ = store.Unlock() }()
	st, err := store.Load()
	require.NoError(t, err)
	assert.NotNil(t, st.Tools["test-tool"])
	assert.Equal(t, "1.0.0", st.Tools["test-tool"].Version)
}

func TestEngine_Apply_NoChanges(t *testing.T) {
	// Create test config directory with CUE file
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "tools.cue")
	cueContent := `package toto

tool: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "test-tool"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/test-tool.tar.gz"
			checksum: {
				value: "sha256:abc123"
			}
			archiveType: "tar.gz"
		}
	}
}
`
	err := os.WriteFile(cueFile, []byte(cueContent), 0644)
	require.NoError(t, err)

	// Setup mock and store with pre-existing state
	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// Pre-populate state with matching version
	require.NoError(t, store.Lock())
	initialState := state.NewUserState()
	initialState.Tools["test-tool"] = &resource.ToolState{
		InstallerRef: "download",
		Version:      "1.0.0",
		InstallPath:  "/tools/test-tool/1.0.0",
		BinPath:      "/bin/test-tool",
	}
	err = store.Save(initialState)
	require.NoError(t, err)
	_ = store.Unlock()

	installCalled := false
	mock := &mockToolInstaller{
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			installCalled = true
			return nil, nil
		},
	}

	engine := NewEngine(mock, store)

	// Run Apply
	err = engine.Apply(context.Background(), configDir)
	require.NoError(t, err)

	// Install should not be called since version matches
	assert.False(t, installCalled)
}

func TestEngine_Plan(t *testing.T) {
	// Create test config directory with CUE file
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "tools.cue")
	cueContent := `package toto

tool: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "test-tool"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/test-tool.tar.gz"
			checksum: {
				value: "sha256:abc123"
			}
			archiveType: "tar.gz"
		}
	}
}
`
	err := os.WriteFile(cueFile, []byte(cueContent), 0644)
	require.NoError(t, err)

	// Setup store
	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mock := &mockToolInstaller{}
	engine := NewEngine(mock, store)

	// Run Plan
	actions, err := engine.Plan(context.Background(), configDir)
	require.NoError(t, err)

	// Should have one install action
	require.Len(t, actions, 1)
	assert.Equal(t, resource.ActionInstall, actions[0].Type)
	assert.Equal(t, "test-tool", actions[0].Name)
}
