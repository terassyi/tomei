//go:build integration

package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/tomei/internal/env"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/state"
)

func TestEnv_GenerateFromState(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	// Create temp directory with state.json
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0755))

	userState := &state.UserState{
		Version: "1",
		Runtimes: map[string]*resource.RuntimeState{
			"go": {
				Type:        resource.InstallTypeDownload,
				Version:     "1.25.6",
				InstallPath: filepath.Join(home, ".local/share/tomei/runtimes/go/1.25.6"),
				BinDir:      filepath.Join(home, "go/bin"),
				ToolBinPath: filepath.Join(home, "go/bin"),
				Env: map[string]string{
					"GOROOT": filepath.Join(home, ".local/share/tomei/runtimes/go/1.25.6"),
					"GOBIN":  filepath.Join(home, "go/bin"),
				},
			},
		},
		Tools: make(map[string]*resource.ToolState),
	}

	// Write state.json and load it back via state.Store
	data, err := json.MarshalIndent(userState, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "state.json"), data, 0644))

	store, err := state.NewStore[state.UserState](dataDir)
	require.NoError(t, err)
	require.NoError(t, store.Lock())
	defer func() { _ = store.Unlock() }()

	loaded, err := store.Load()
	require.NoError(t, err)

	// Generate from loaded state
	userBinDir := filepath.Join(home, ".local/bin")

	t.Run("posix", func(t *testing.T) {
		f := env.NewFormatter(env.ShellPosix)
		lines := env.Generate(loaded.Runtimes, userBinDir, f)
		assert.NotEmpty(t, lines)

		output := joinLines(lines)
		assert.Contains(t, output, `export GOROOT=`)
		assert.Contains(t, output, `export GOBIN=`)
		assert.Contains(t, output, `export PATH=`)
	})

	t.Run("fish", func(t *testing.T) {
		f := env.NewFormatter(env.ShellFish)
		lines := env.Generate(loaded.Runtimes, userBinDir, f)
		assert.NotEmpty(t, lines)

		output := joinLines(lines)
		assert.Contains(t, output, `set -gx GOROOT`)
		assert.Contains(t, output, `fish_add_path`)
	})
}

func TestGenerate_MultipleRuntimes(t *testing.T) {
	runtimes := map[string]*resource.RuntimeState{
		"go": {
			Type:        resource.InstallTypeDownload,
			Version:     "1.25.6",
			InstallPath: "/opt/go/1.25.6",
			BinDir:      "/opt/go/1.25.6/bin",
			ToolBinPath: "/home/user/go/bin",
			Env: map[string]string{
				"GOROOT": "/opt/go/1.25.6",
			},
		},
		"rust": {
			Type:        resource.InstallTypeDownload,
			Version:     "1.85.0",
			InstallPath: "/opt/rust/1.85.0",
			BinDir:      "/opt/rust/1.85.0/bin",
			ToolBinPath: "/home/user/.cargo/bin",
			Env: map[string]string{
				"RUSTUP_HOME": "/opt/rust/1.85.0",
			},
		},
	}

	userBinDir := "/home/user/.local/bin"
	f := env.NewFormatter(env.ShellPosix)
	lines := env.Generate(runtimes, userBinDir, f)
	assert.NotEmpty(t, lines)

	output := joinLines(lines)

	// Verify PATH contains entries for all runtimes
	assert.Contains(t, output, "export PATH=")
	assert.Contains(t, output, "/opt/go/1.25.6/bin")
	assert.Contains(t, output, "/home/user/go/bin")
	assert.Contains(t, output, "/opt/rust/1.85.0/bin")
	assert.Contains(t, output, "/home/user/.cargo/bin")
	assert.Contains(t, output, "/home/user/.local/bin")

	// Verify env vars from each runtime are present
	assert.Contains(t, output, "export GOROOT=")
	assert.Contains(t, output, "export RUSTUP_HOME=")

	// Verify deterministic ordering: run twice and compare
	lines2 := env.Generate(runtimes, userBinDir, f)
	assert.Equal(t, lines, lines2, "output should be deterministic across calls")

	// Verify "go" env vars come before "rust" env vars (sorted by runtime name)
	gorootIdx := -1
	rustupIdx := -1
	for i, l := range lines {
		if gorootIdx < 0 && strings.HasPrefix(l, "export GOROOT=") {
			gorootIdx = i
		}
		if rustupIdx < 0 && strings.HasPrefix(l, "export RUSTUP_HOME=") {
			rustupIdx = i
		}
	}
	if gorootIdx >= 0 && rustupIdx >= 0 {
		assert.Less(t, gorootIdx, rustupIdx, "go env vars should come before rust env vars (alphabetical)")
	}
}

func TestGenerate_EmptyRuntimes(t *testing.T) {
	runtimes := map[string]*resource.RuntimeState{}

	userBinDir := "/home/user/.local/bin"
	f := env.NewFormatter(env.ShellPosix)
	lines := env.Generate(runtimes, userBinDir, f)

	// Should only contain the PATH entry with userBinDir
	require.Len(t, lines, 1, "should only contain the PATH export")
	assert.Contains(t, lines[0], "/home/user/.local/bin")
	assert.Contains(t, lines[0], "export PATH=")
}

func joinLines(lines []string) string {
	result := ""
	for _, l := range lines {
		result += l + "\n"
	}
	return result
}
