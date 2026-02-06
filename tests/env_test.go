//go:build integration

package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/toto/internal/env"
	"github.com/terassyi/toto/internal/resource"
	"github.com/terassyi/toto/internal/state"
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
				InstallPath: filepath.Join(home, ".local/share/toto/runtimes/go/1.25.6"),
				BinDir:      filepath.Join(home, "go/bin"),
				ToolBinPath: filepath.Join(home, "go/bin"),
				Env: map[string]string{
					"GOROOT": filepath.Join(home, ".local/share/toto/runtimes/go/1.25.6"),
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

func joinLines(lines []string) string {
	result := ""
	for _, l := range lines {
		result += l + "\n"
	}
	return result
}
