package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, DefaultDataDir, cfg.DataDir)
	assert.Equal(t, DefaultBinDir, cfg.BinDir)
}

func TestLoadConfig_NoConfigFile(t *testing.T) {
	tmpDir := t.TempDir()

	cfg, err := LoadConfig(tmpDir)
	require.NoError(t, err)

	// Should return default config
	assert.Equal(t, DefaultDataDir, cfg.DataDir)
	assert.Equal(t, DefaultBinDir, cfg.BinDir)
}

func TestLoadConfig_WithConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.cue")

	cueContent := `package toto

config: {
    dataDir: "~/my-data"
    binDir: "~/my-bin"
}
`
	err := os.WriteFile(configPath, []byte(cueContent), 0644)
	require.NoError(t, err)

	cfg, err := LoadConfig(tmpDir)
	require.NoError(t, err)

	assert.Equal(t, "~/my-data", cfg.DataDir)
	assert.Equal(t, "~/my-bin", cfg.BinDir)
}

func TestLoadConfig_PartialConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.cue")

	// Only dataDir specified
	cueContent := `package toto

config: {
    dataDir: "~/custom-data"
}
`
	err := os.WriteFile(configPath, []byte(cueContent), 0644)
	require.NoError(t, err)

	cfg, err := LoadConfig(tmpDir)
	require.NoError(t, err)

	assert.Equal(t, "~/custom-data", cfg.DataDir)
	// binDir should be default
	assert.Equal(t, DefaultBinDir, cfg.BinDir)
}

func TestLoadConfig_NoConfigBlock(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.cue")

	// config.cue exists but has no config block
	cueContent := `package toto

somethingElse: "value"
`
	err := os.WriteFile(configPath, []byte(cueContent), 0644)
	require.NoError(t, err)

	cfg, err := LoadConfig(tmpDir)
	require.NoError(t, err)

	// Should return default config
	assert.Equal(t, DefaultDataDir, cfg.DataDir)
	assert.Equal(t, DefaultBinDir, cfg.BinDir)
}

func TestLoadConfig_InvalidCue(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.cue")

	// Invalid CUE syntax
	cueContent := `package toto
config: {
    dataDir: "~/my-data"
    // missing closing brace
`
	err := os.WriteFile(configPath, []byte(cueContent), 0644)
	require.NoError(t, err)

	_, err = LoadConfig(tmpDir)
	assert.Error(t, err)
}

func TestConfig_ToCue(t *testing.T) {
	cfg := &Config{
		DataDir: "~/my-data",
		BinDir:  "~/my-bin",
	}

	cueBytes, err := cfg.ToCue()
	require.NoError(t, err)

	cueContent := string(cueBytes)
	assert.Contains(t, cueContent, "package toto")
	assert.Contains(t, cueContent, "config")
	assert.Contains(t, cueContent, "dataDir")
	assert.Contains(t, cueContent, "~/my-data")
	assert.Contains(t, cueContent, "binDir")
	assert.Contains(t, cueContent, "~/my-bin")
}

func TestConfig_ToCue_RoundTrip(t *testing.T) {
	// Generate CUE from default config
	cfg := DefaultConfig()
	cueBytes, err := cfg.ToCue()
	require.NoError(t, err)

	// Write to temp dir and load back
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.cue")
	err = os.WriteFile(configPath, cueBytes, 0644)
	require.NoError(t, err)

	// Load and verify
	loadedCfg, err := LoadConfig(tmpDir)
	require.NoError(t, err)

	assert.Equal(t, cfg.DataDir, loadedCfg.DataDir)
	assert.Equal(t, cfg.BinDir, loadedCfg.BinDir)
}
