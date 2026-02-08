package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/load"
	"github.com/terassyi/tomei/internal/config/schema"
)

// Default path constants
const (
	DefaultConfigDir = "~/.config/tomei"
	DefaultDataDir   = "~/.local/share/tomei"
	DefaultBinDir    = "~/.local/bin"
	DefaultEnvDir    = "~/.config/tomei"
	SchemaFileName   = "schema.cue"
)

// Config represents tomei configuration.
type Config struct {
	DataDir   string `json:"dataDir"`
	BinDir    string `json:"binDir"`
	EnvDir    string `json:"envDir"`
	SchemaDir string `json:"schemaDir,omitempty"`
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

	return append([]byte("package tomei\n\n"), b...), nil
}

// expandHome expands ~ to the user's home directory.
// This is a local copy to avoid circular imports with internal/path.
func expandHome(p string) (string, error) {
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, p[2:]), nil
	}
	if p == "~" {
		return os.UserHomeDir()
	}
	return p, nil
}

// SyncSchema compares the embedded schema with the schema file on disk
// and updates the file if they differ. The schema directory is determined
// from config.SchemaDir (if set) or falls back to the config directory.
// If the schema file does not exist yet (init not run), it returns nil.
func SyncSchema(cfg *Config, configDir string) error {
	dir := configDir
	if cfg.SchemaDir != "" {
		dir = cfg.SchemaDir
	}

	expanded, err := expandHome(dir)
	if err != nil {
		return fmt.Errorf("failed to expand schema directory: %w", err)
	}

	schemaFile := filepath.Join(expanded, SchemaFileName)
	existing, err := os.ReadFile(schemaFile)
	if err != nil {
		// File doesn't exist yet; skip (init places it)
		return nil
	}

	if string(existing) == schema.SchemaCUE {
		return nil // already up to date
	}

	if err := os.WriteFile(schemaFile, []byte(schema.SchemaCUE), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", schemaFile, err)
	}

	slog.Info("schema.cue updated", "path", schemaFile)
	return nil
}
