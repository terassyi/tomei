package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/load"
)

// Default path constants
const (
	DefaultConfigDir = "~/.config/toto"
	DefaultDataDir   = "~/.local/share/toto"
	DefaultBinDir    = "~/.local/bin"
	DefaultEnvDir    = "~/.config/toto"
)

// Config represents toto configuration.
type Config struct {
	DataDir string `json:"dataDir"`
	BinDir  string `json:"binDir"`
	EnvDir  string `json:"envDir"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		DataDir: DefaultDataDir,
		BinDir:  DefaultBinDir,
		EnvDir:  DefaultEnvDir,
	}
}

// LoadConfig loads configuration from the config directory.
// Returns default config if config.cue doesn't exist or has no config block.
func LoadConfig(configDir string) (*Config, error) {
	configPath := filepath.Join(configDir, "config.cue")

	// Check if config.cue exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return DefaultConfig(), nil
	}

	// Load CUE file
	ctx := cuecontext.New()
	instances := load.Instances([]string{"config.cue"}, &load.Config{
		Dir: configDir,
	})

	if len(instances) == 0 {
		return DefaultConfig(), nil
	}

	inst := instances[0]
	if inst.Err != nil {
		return nil, fmt.Errorf("failed to load config.cue: %w", inst.Err)
	}

	value := ctx.BuildInstance(inst)
	if value.Err() != nil {
		return nil, fmt.Errorf("failed to build config.cue: %w", value.Err())
	}

	// Look for config block
	configValue := value.LookupPath(cue.ParsePath("config"))
	if !configValue.Exists() {
		return DefaultConfig(), nil
	}

	// Parse config
	cfg := DefaultConfig()
	jsonBytes, err := configValue.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := json.Unmarshal(jsonBytes, cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return cfg, nil
}

// ToCue generates CUE content from Config.
func (c *Config) ToCue() ([]byte, error) {
	ctx := cuecontext.New()
	v := ctx.Encode(map[string]any{
		"config": c,
	})
	if v.Err() != nil {
		return nil, fmt.Errorf("failed to encode config: %w", v.Err())
	}

	syn := v.Syntax()
	b, err := format.Node(syn)
	if err != nil {
		return nil, fmt.Errorf("failed to format config: %w", err)
	}

	return append([]byte("package toto\n\n"), b...), nil
}
