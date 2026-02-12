package config

import (
	"encoding/json"
	"fmt"
	"io"
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

// SchemaResult indicates the action taken by WriteSchema.
type SchemaResult int

const (
	// SchemaCreated means schema.cue was newly created.
	SchemaCreated SchemaResult = iota
	// SchemaUpdated means schema.cue existed but was outdated and has been updated.
	SchemaUpdated
	// SchemaUpToDate means schema.cue already matched the embedded schema.
	SchemaUpToDate
)

// userSchemaCUE returns the schema content with the package declaration
// set to "tomei" for user-facing schema.cue files (CUE language server support).
func userSchemaCUE() string {
	return strings.Replace(schema.SchemaCUE, "package schema", "package tomei", 1)
}

// WriteSchema writes the embedded schema.cue to the given directory.
// It creates the directory if it does not exist.
// The written file uses "package tomei" for CUE language server compatibility.
func WriteSchema(dir string) (SchemaResult, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return 0, fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	content := userSchemaCUE()
	schemaFile := filepath.Join(dir, SchemaFileName)

	existing, err := os.ReadFile(schemaFile)
	if err != nil {
		if !os.IsNotExist(err) {
			return 0, fmt.Errorf("failed to read %s: %w", schemaFile, err)
		}
		if err := os.WriteFile(schemaFile, []byte(content), 0644); err != nil {
			return 0, fmt.Errorf("failed to write %s: %w", schemaFile, err)
		}
		return SchemaCreated, nil
	}

	if string(existing) == content {
		return SchemaUpToDate, nil
	}

	if err := os.WriteFile(schemaFile, []byte(content), 0644); err != nil {
		return 0, fmt.Errorf("failed to write %s: %w", schemaFile, err)
	}
	return SchemaUpdated, nil
}

// CheckSchemaVersion checks whether the schema.cue in the given directory
// has the same apiVersion as the embedded schema. If schema.cue does not
// exist, the check is skipped (nil is returned). If the apiVersion differs,
// an error is returned advising the user to run 'tomei schema'.
func CheckSchemaVersion(dir string) error {
	schemaFile := filepath.Join(dir, SchemaFileName)
	f, err := os.Open(schemaFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no schema.cue — skip check
		}
		return fmt.Errorf("failed to open %s: %w", schemaFile, err)
	}
	defer f.Close()

	fileVersion := extractAPIVersion(f)
	if fileVersion == "" {
		return nil // no #APIVersion found — skip check
	}

	embeddedVersion := extractAPIVersionFromString(schema.SchemaCUE)
	if embeddedVersion == "" {
		return nil // should not happen
	}

	if fileVersion != embeddedVersion {
		return fmt.Errorf("schema.cue apiVersion mismatch: expected %q, got %q. Run 'tomei schema' to update", embeddedVersion, fileVersion)
	}
	return nil
}

// extractAPIVersion reads the file and returns the #APIVersion value if found.
func extractAPIVersion(f *os.File) string {
	b, err := io.ReadAll(f)
	if err != nil {
		return ""
	}
	return extractAPIVersionFromString(string(b))
}

// extractAPIVersionFromString extracts #APIVersion from a CUE source string.
func extractAPIVersionFromString(src string) string {
	for line := range strings.SplitSeq(src, "\n") {
		line = strings.TrimSpace(line)
		if v, ok := parseAPIVersionLine(line); ok {
			return v
		}
	}
	return ""
}

// parseAPIVersionLine parses a line like '#APIVersion: "tomei.terassyi.net/v1beta1"'
// and returns the unquoted value.
func parseAPIVersionLine(line string) (string, bool) {
	if !strings.HasPrefix(line, "#APIVersion:") {
		return "", false
	}
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return "", false
	}
	v := strings.TrimSpace(parts[1])
	v = strings.Trim(v, `"`)
	return v, true
}

// expandTilde replaces a leading ~/ with the user's home directory.
func expandTilde(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		return filepath.Join(home, p[2:])
	}
	return p
}

// CheckSchemaVersionForPaths checks schema.cue apiVersion for each
// manifest path (file or directory). Directories that don't contain
// schema.cue are silently skipped.
func CheckSchemaVersionForPaths(paths []string) error {
	checked := make(map[string]struct{})
	for _, p := range paths {
		p = expandTilde(p)
		dir := p
		info, err := os.Stat(p)
		if err != nil {
			continue // will be caught later by loader
		}
		if !info.IsDir() {
			dir = filepath.Dir(p)
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		if _, ok := checked[abs]; ok {
			continue
		}
		checked[abs] = struct{}{}
		if err := CheckSchemaVersion(abs); err != nil {
			return err
		}
	}
	return nil
}
