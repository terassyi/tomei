package path

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	tests := []struct {
		name            string
		opts            []Option
		wantUserDataDir string
		wantUserBinDir  string
		wantSystemDir   string
	}{
		{
			name:            "default paths",
			opts:            nil,
			wantUserDataDir: filepath.Join(home, ".local/share/toto"),
			wantUserBinDir:  filepath.Join(home, ".local/bin"),
			wantSystemDir:   DefaultSystemDataDir,
		},
		{
			name:            "with custom user data dir",
			opts:            []Option{WithUserDataDir("/custom/data")},
			wantUserDataDir: "/custom/data",
			wantUserBinDir:  filepath.Join(home, ".local/bin"),
			wantSystemDir:   DefaultSystemDataDir,
		},
		{
			name:            "with custom user bin dir",
			opts:            []Option{WithUserBinDir("/custom/bin")},
			wantUserDataDir: filepath.Join(home, ".local/share/toto"),
			wantUserBinDir:  "/custom/bin",
			wantSystemDir:   DefaultSystemDataDir,
		},
		{
			name:            "with custom system data dir",
			opts:            []Option{WithSystemDataDir("/custom/system")},
			wantUserDataDir: filepath.Join(home, ".local/share/toto"),
			wantUserBinDir:  filepath.Join(home, ".local/bin"),
			wantSystemDir:   "/custom/system",
		},
		{
			name: "with all custom dirs",
			opts: []Option{
				WithUserDataDir("/custom/data"),
				WithUserBinDir("/custom/bin"),
				WithSystemDataDir("/custom/system"),
			},
			wantUserDataDir: "/custom/data",
			wantUserBinDir:  "/custom/bin",
			wantSystemDir:   "/custom/system",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := New(tt.opts...)
			require.NoError(t, err)

			assert.Equal(t, tt.wantUserDataDir, p.UserDataDir())
			assert.Equal(t, tt.wantUserBinDir, p.UserBinDir())
			assert.Equal(t, tt.wantSystemDir, p.SystemDataDir())
		})
	}
}

func TestPaths_ToolInstallDir(t *testing.T) {
	tests := []struct {
		name        string
		userDataDir string
		toolName    string
		version     string
		want        string
	}{
		{
			name:        "ripgrep",
			userDataDir: "/data",
			toolName:    "ripgrep",
			version:     "14.1.1",
			want:        "/data/tools/ripgrep/14.1.1",
		},
		{
			name:        "fd",
			userDataDir: "/home/user/.local/share/toto",
			toolName:    "fd",
			version:     "9.0.0",
			want:        "/home/user/.local/share/toto/tools/fd/9.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := New(WithUserDataDir(tt.userDataDir))
			require.NoError(t, err)

			got := p.ToolInstallDir(tt.toolName, tt.version)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPaths_RuntimeInstallDir(t *testing.T) {
	tests := []struct {
		name        string
		userDataDir string
		runtime     string
		version     string
		want        string
	}{
		{
			name:        "go",
			userDataDir: "/data",
			runtime:     "go",
			version:     "1.25.6",
			want:        "/data/runtimes/go/1.25.6",
		},
		{
			name:        "rust",
			userDataDir: "/data",
			runtime:     "rust",
			version:     "1.75.0",
			want:        "/data/runtimes/rust/1.75.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := New(WithUserDataDir(tt.userDataDir))
			require.NoError(t, err)

			got := p.RuntimeInstallDir(tt.runtime, tt.version)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPaths_StateFiles(t *testing.T) {
	tests := []struct {
		name          string
		userDataDir   string
		systemDataDir string
		wantUserState string
		wantUserLock  string
		wantSysState  string
		wantSysLock   string
	}{
		{
			name:          "custom dirs",
			userDataDir:   "/data",
			systemDataDir: "/system",
			wantUserState: "/data/state.json",
			wantUserLock:  "/data/state.lock",
			wantSysState:  "/system/state.json",
			wantSysLock:   "/system/state.lock",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := New(
				WithUserDataDir(tt.userDataDir),
				WithSystemDataDir(tt.systemDataDir),
			)
			require.NoError(t, err)

			assert.Equal(t, tt.wantUserState, p.UserStateFile())
			assert.Equal(t, tt.wantUserLock, p.UserStateLockFile())
			assert.Equal(t, tt.wantSysState, p.SystemStateFile())
			assert.Equal(t, tt.wantSysLock, p.SystemStateLockFile())
		})
	}
}

func TestEnsureDir(t *testing.T) {
	tests := []struct {
		name    string
		subPath string
	}{
		{
			name:    "single level",
			subPath: "a",
		},
		{
			name:    "nested levels",
			subPath: "a/b/c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			targetDir := filepath.Join(tmpDir, tt.subPath)

			err := EnsureDir(targetDir)
			require.NoError(t, err)

			info, err := os.Stat(targetDir)
			require.NoError(t, err)
			assert.True(t, info.IsDir())
		})
	}
}
