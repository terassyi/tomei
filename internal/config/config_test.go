package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/config/schema"
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

	cueContent := `package tomei

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
	cueContent := `package tomei

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
	cueContent := `package tomei

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
	cueContent := `package tomei
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
		EnvDir:  "~/my-env",
	}

	cueBytes, err := cfg.ToCue()
	require.NoError(t, err)

	cueContent := string(cueBytes)
	assert.Contains(t, cueContent, "package tomei")
	assert.Contains(t, cueContent, "config")
	assert.Contains(t, cueContent, "dataDir")
	assert.Contains(t, cueContent, "~/my-data")
	assert.Contains(t, cueContent, "binDir")
	assert.Contains(t, cueContent, "~/my-bin")
	assert.Contains(t, cueContent, "envDir")
	assert.Contains(t, cueContent, "~/my-env")
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
	assert.Equal(t, cfg.EnvDir, loadedCfg.EnvDir)
}

func TestParseAPIVersionLine(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		want   string
		wantOK bool
	}{
		{
			name:   "valid v1beta1",
			line:   `#APIVersion: "tomei.terassyi.net/v1beta1"`,
			want:   "tomei.terassyi.net/v1beta1",
			wantOK: true,
		},
		{
			name:   "valid v1",
			line:   `#APIVersion: "tomei.terassyi.net/v1"`,
			want:   "tomei.terassyi.net/v1",
			wantOK: true,
		},
		{
			name:   "with extra whitespace",
			line:   `  #APIVersion:   "tomei.terassyi.net/v1beta1"  `,
			want:   "",
			wantOK: false, // leading whitespace means HasPrefix fails after TrimSpace at call site
		},
		{
			name:   "not APIVersion line",
			line:   `#Metadata: { name: string }`,
			want:   "",
			wantOK: false,
		},
		{
			name:   "empty line",
			line:   "",
			want:   "",
			wantOK: false,
		},
		{
			name:   "comment line",
			line:   "// #APIVersion is defined here",
			want:   "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseAPIVersionLine(tt.line)
			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestExtractAPIVersionFromString(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "standard schema",
			src: `package tomei

#APIVersion: "tomei.terassyi.net/v1beta1"

#Metadata: {
    name: string
}`,
			want: "tomei.terassyi.net/v1beta1",
		},
		{
			name: "no APIVersion",
			src: `package tomei

#Metadata: {
    name: string
}`,
			want: "",
		},
		{
			name: "empty string",
			src:  "",
			want: "",
		},
		{
			name: "APIVersion in comment is ignored",
			src: `package tomei

// #APIVersion: "tomei.terassyi.net/v1beta1"
`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAPIVersionFromString(tt.src)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestWriteSchema(t *testing.T) {
	tests := []struct {
		name         string
		existingFile bool
		content      string
		wantResult   SchemaResult
	}{
		{
			name:       "creates schema.cue when not present",
			wantResult: SchemaCreated,
		},
		{
			name:         "no change when content matches",
			existingFile: true,
			content:      schema.SchemaCUE,
			wantResult:   SchemaUpToDate,
		},
		{
			name:         "updates when content differs",
			existingFile: true,
			content:      "package tomei\n\n// outdated schema\n",
			wantResult:   SchemaUpdated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			if tt.existingFile {
				err := os.WriteFile(filepath.Join(tmpDir, SchemaFileName), []byte(tt.content), 0644)
				require.NoError(t, err)
			}

			result, err := WriteSchema(tmpDir)
			require.NoError(t, err)
			assert.Equal(t, tt.wantResult, result)

			// Verify file content always matches embedded schema
			got, err := os.ReadFile(filepath.Join(tmpDir, SchemaFileName))
			require.NoError(t, err)
			assert.Equal(t, schema.SchemaCUE, string(got))
		})
	}
}

func TestWriteSchema_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	newDir := filepath.Join(tmpDir, "subdir", "nested")

	result, err := WriteSchema(newDir)
	require.NoError(t, err)
	assert.Equal(t, SchemaCreated, result)

	got, err := os.ReadFile(filepath.Join(newDir, SchemaFileName))
	require.NoError(t, err)
	assert.Equal(t, schema.SchemaCUE, string(got))
}

func TestCheckSchemaVersion(t *testing.T) {
	tests := []struct {
		name       string
		setupFile  bool
		content    string
		wantErr    bool
		errContain string
	}{
		{
			name:      "no schema.cue — skip",
			setupFile: false,
			wantErr:   false,
		},
		{
			name:      "matching version — ok",
			setupFile: true,
			content: `package tomei

#APIVersion: "tomei.terassyi.net/v1beta1"
`,
			wantErr: false,
		},
		{
			name:      "mismatched version — error",
			setupFile: true,
			content: `package tomei

#APIVersion: "tomei.terassyi.net/v0alpha1"
`,
			wantErr:    true,
			errContain: "apiVersion mismatch",
		},
		{
			name:      "no APIVersion in file — skip",
			setupFile: true,
			content: `package tomei

#Metadata: {
    name: string
}
`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			if tt.setupFile {
				err := os.WriteFile(filepath.Join(tmpDir, SchemaFileName), []byte(tt.content), 0644)
				require.NoError(t, err)
			}

			err := CheckSchemaVersion(tmpDir)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContain)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCheckSchemaVersionForPaths(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T) []string
		wantErr    bool
		errContain string
	}{
		{
			name: "no schema.cue in directory — ok",
			setup: func(t *testing.T) []string {
				return []string{t.TempDir()}
			},
		},
		{
			name: "matching schema.cue — ok",
			setup: func(t *testing.T) []string {
				dir := t.TempDir()
				content := "package tomei\n\n#APIVersion: \"tomei.terassyi.net/v1beta1\"\n"
				require.NoError(t, os.WriteFile(filepath.Join(dir, SchemaFileName), []byte(content), 0644))
				return []string{dir}
			},
		},
		{
			name: "mismatched schema.cue — error",
			setup: func(t *testing.T) []string {
				dir := t.TempDir()
				content := "package tomei\n\n#APIVersion: \"tomei.terassyi.net/v0old\"\n"
				require.NoError(t, os.WriteFile(filepath.Join(dir, SchemaFileName), []byte(content), 0644))
				return []string{dir}
			},
			wantErr:    true,
			errContain: "apiVersion mismatch",
		},
		{
			name: "file path resolves to parent directory",
			setup: func(t *testing.T) []string {
				dir := t.TempDir()
				content := "package tomei\n\n#APIVersion: \"tomei.terassyi.net/v1beta1\"\n"
				require.NoError(t, os.WriteFile(filepath.Join(dir, SchemaFileName), []byte(content), 0644))
				filePath := filepath.Join(dir, "tools.cue")
				require.NoError(t, os.WriteFile(filePath, []byte(""), 0644))
				return []string{filePath}
			},
		},
		{
			name: "deduplicates directories",
			setup: func(t *testing.T) []string {
				dir := t.TempDir()
				content := "package tomei\n\n#APIVersion: \"tomei.terassyi.net/v1beta1\"\n"
				require.NoError(t, os.WriteFile(filepath.Join(dir, SchemaFileName), []byte(content), 0644))
				file1 := filepath.Join(dir, "a.cue")
				file2 := filepath.Join(dir, "b.cue")
				require.NoError(t, os.WriteFile(file1, []byte(""), 0644))
				require.NoError(t, os.WriteFile(file2, []byte(""), 0644))
				return []string{file1, file2}
			},
		},
		{
			name: "nonexistent path — skip gracefully",
			setup: func(_ *testing.T) []string {
				return []string{"/nonexistent/path/to/manifest.cue"}
			},
		},
		{
			name: "tilde path expands to home directory",
			setup: func(t *testing.T) []string {
				home, err := os.UserHomeDir()
				require.NoError(t, err)
				dir, err := os.MkdirTemp(home, "tomei-test-*")
				require.NoError(t, err)
				t.Cleanup(func() { os.RemoveAll(dir) })
				content := "package tomei\n\n#APIVersion: \"tomei.terassyi.net/v1beta1\"\n"
				require.NoError(t, os.WriteFile(filepath.Join(dir, SchemaFileName), []byte(content), 0644))
				return []string{"~/" + filepath.Base(dir)}
			},
		},
		{
			name: "tilde path with mismatched schema — error",
			setup: func(t *testing.T) []string {
				home, err := os.UserHomeDir()
				require.NoError(t, err)
				dir, err := os.MkdirTemp(home, "tomei-test-*")
				require.NoError(t, err)
				t.Cleanup(func() { os.RemoveAll(dir) })
				content := "#APIVersion: \"wrong/v1\"\n"
				require.NoError(t, os.WriteFile(filepath.Join(dir, SchemaFileName), []byte(content), 0644))
				return []string{"~/" + filepath.Base(dir)}
			},
			wantErr:    true,
			errContain: "apiVersion mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths := tt.setup(t)
			err := CheckSchemaVersionForPaths(paths)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContain)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestExpandTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "tilde prefix",
			path: "~/Documents",
			want: filepath.Join(home, "Documents"),
		},
		{
			name: "tilde only slash",
			path: "~/",
			want: home,
		},
		{
			name: "absolute path unchanged",
			path: "/usr/local/bin",
			want: "/usr/local/bin",
		},
		{
			name: "relative path unchanged",
			path: "relative/path",
			want: "relative/path",
		},
		{
			name: "empty string",
			path: "",
			want: "",
		},
		{
			name: "tilde without slash",
			path: "~other",
			want: "~other",
		},
		{
			name: "nested tilde path",
			path: "~/a/b/c",
			want: filepath.Join(home, "a/b/c"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expandTilde(tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractAPIVersion(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "valid apiVersion",
			content: "#APIVersion: \"tomei.terassyi.net/v1beta1\"\n",
			want:    "tomei.terassyi.net/v1beta1",
		},
		{
			name:    "no apiVersion",
			content: "package tomei\n",
			want:    "",
		},
		{
			name:    "empty file",
			content: "",
			want:    "",
		},
		{
			name: "apiVersion among other content",
			content: `package tomei

#APIVersion: "tomei.terassyi.net/v1beta1"

#Metadata: {
    name: string
}
`,
			want: "tomei.terassyi.net/v1beta1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			filePath := filepath.Join(tmpDir, "test.cue")
			require.NoError(t, os.WriteFile(filePath, []byte(tt.content), 0644))

			f, err := os.Open(filePath)
			require.NoError(t, err)
			defer f.Close()

			got := extractAPIVersion(f)
			assert.Equal(t, tt.want, got)
		})
	}
}
