package place

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPlacer(t *testing.T) {
	t.Parallel()
	p := NewPlacer("/tools")
	assert.NotNil(t, p)
}

func TestValidateAction_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		action ValidateAction
		want   string
	}{
		{ValidateActionInstall, "install"},
		{ValidateActionSkip, "skip"},
		{ValidateActionReplace, "replace"},
		{ValidateAction(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.action.String())
		})
	}
}

func TestPlacer_Validate(t *testing.T) {
	t.Parallel()
	content := []byte("binary content")
	contentHash := sha256Hash(content)

	tests := []struct {
		name         string
		setup        func(t *testing.T, toolsDir string, target Target)
		target       Target
		expectedHash string
		wantAction   ValidateAction
		wantErr      bool
	}{
		{
			name:         "binary does not exist - install",
			setup:        func(t *testing.T, toolsDir string, target Target) {},
			target:       Target{Name: "mytool", Version: "1.0.0", BinaryName: "tool"},
			expectedHash: contentHash,
			wantAction:   ValidateActionInstall,
			wantErr:      false,
		},
		{
			name: "binary exists with matching hash - skip",
			setup: func(t *testing.T, toolsDir string, target Target) {
				binPath := filepath.Join(toolsDir, target.Name, target.Version, target.BinaryName)
				err := os.MkdirAll(filepath.Dir(binPath), 0755)
				require.NoError(t, err)
				err = os.WriteFile(binPath, content, 0755)
				require.NoError(t, err)
			},
			target:       Target{Name: "mytool", Version: "1.0.0", BinaryName: "tool"},
			expectedHash: contentHash,
			wantAction:   ValidateActionSkip,
			wantErr:      false,
		},
		{
			name: "binary exists with different hash - replace",
			setup: func(t *testing.T, toolsDir string, target Target) {
				binPath := filepath.Join(toolsDir, target.Name, target.Version, target.BinaryName)
				err := os.MkdirAll(filepath.Dir(binPath), 0755)
				require.NoError(t, err)
				err = os.WriteFile(binPath, []byte("different content"), 0755)
				require.NoError(t, err)
			},
			target:       Target{Name: "mytool", Version: "1.0.0", BinaryName: "tool"},
			expectedHash: contentHash,
			wantAction:   ValidateActionReplace,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			toolsDir := filepath.Join(tmpDir, "tools")

			tt.setup(t, toolsDir, tt.target)

			p := NewPlacer(toolsDir)
			action, err := p.Validate(tt.target, tt.expectedHash)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantAction, action)
		})
	}
}

func TestPlacer_Place(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		setup      func(t *testing.T, srcDir string)
		target     Target
		wantErr    bool
		errContain string
	}{
		{
			name: "place single binary",
			setup: func(t *testing.T, srcDir string) {
				binPath := filepath.Join(srcDir, "tool")
				err := os.WriteFile(binPath, []byte("binary content"), 0755)
				require.NoError(t, err)
			},
			target: Target{
				Name:       "mytool",
				Version:    "1.0.0",
				BinaryName: "tool",
			},
			wantErr: false,
		},
		{
			name: "place non-executable source binary (zip 0644) is made executable",
			setup: func(t *testing.T, srcDir string) {
				binPath := filepath.Join(srcDir, "tool")
				// zip stores 0644; the placed tool binary must still be runnable (#273).
				err := os.WriteFile(binPath, []byte("binary content"), 0o644)
				require.NoError(t, err)
			},
			target: Target{
				Name:       "mytool",
				Version:    "1.0.0",
				BinaryName: "tool",
			},
			wantErr: false,
		},
		{
			name: "place binary from nested directory",
			setup: func(t *testing.T, srcDir string) {
				// Create nested structure like GitHub releases
				nestedDir := filepath.Join(srcDir, "ripgrep-14.0.0")
				err := os.MkdirAll(nestedDir, 0755)
				require.NoError(t, err)
				binPath := filepath.Join(nestedDir, "rg")
				err = os.WriteFile(binPath, []byte("binary content"), 0755)
				require.NoError(t, err)
			},
			target: Target{
				Name:       "ripgrep",
				Version:    "14.0.0",
				BinaryName: "rg",
			},
			wantErr: false,
		},
		{
			name: "place binary with SrcBinaryName mapping",
			setup: func(t *testing.T, srcDir string) {
				// Archive contains "krew-linux_arm64" but we want to place as "krew"
				binPath := filepath.Join(srcDir, "krew-linux_arm64")
				err := os.WriteFile(binPath, []byte("binary content"), 0755)
				require.NoError(t, err)
			},
			target: Target{
				Name:          "krew",
				Version:       "0.4.4",
				BinaryName:    "krew",
				SrcBinaryName: "krew-linux_arm64",
			},
			wantErr: false,
		},
		{
			name: "place binary found by WalkDir in subdirectory (helm pattern)",
			setup: func(t *testing.T, srcDir string) {
				// Archive layout: linux-arm64/helm — installer has already extracted path.Base("linux-arm64/helm") = "helm"
				dir := filepath.Join(srcDir, "linux-arm64")
				require.NoError(t, os.MkdirAll(dir, 0755))
				err := os.WriteFile(filepath.Join(dir, "helm"), []byte("binary content"), 0755)
				require.NoError(t, err)
			},
			target: Target{
				Name:       "helm",
				Version:    "3.16.0",
				BinaryName: "helm",
				// SrcBinaryName is empty because installer already applied path.Base
			},
			wantErr: false,
		},
		{
			name: "place binary found by WalkDir in deep nested directory (gh pattern)",
			setup: func(t *testing.T, srcDir string) {
				// Archive layout: gh_2.86.0_linux_amd64/bin/gh — installer extracts path.Base = "gh"
				dir := filepath.Join(srcDir, "gh_2.86.0_linux_amd64", "bin")
				require.NoError(t, os.MkdirAll(dir, 0755))
				err := os.WriteFile(filepath.Join(dir, "gh"), []byte("binary content"), 0755)
				require.NoError(t, err)
			},
			target: Target{
				Name:       "gh",
				Version:    "2.86.0",
				BinaryName: "gh",
			},
			wantErr: false,
		},
		{
			name: "SrcBinaryName not found in archive",
			setup: func(t *testing.T, srcDir string) {
				// Archive contains different binary name
				binPath := filepath.Join(srcDir, "other-binary")
				err := os.WriteFile(binPath, []byte("binary content"), 0755)
				require.NoError(t, err)
			},
			target: Target{
				Name:          "krew",
				Version:       "0.4.4",
				BinaryName:    "krew",
				SrcBinaryName: "krew-linux_arm64",
			},
			wantErr:    true,
			errContain: "not found",
		},
		{
			name: "binary not found",
			setup: func(t *testing.T, srcDir string) {
				// Empty directory
			},
			target: Target{
				Name:       "mytool",
				Version:    "1.0.0",
				BinaryName: "nonexistent",
			},
			wantErr:    true,
			errContain: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			srcDir := filepath.Join(tmpDir, "src")
			toolsDir := filepath.Join(tmpDir, "tools")

			err := os.MkdirAll(srcDir, 0755)
			require.NoError(t, err)

			tt.setup(t, srcDir)

			p := NewPlacer(toolsDir)
			result, err := p.Place(srcDir, tt.target)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContain != "" {
					assert.Contains(t, err.Error(), tt.errContain)
				}
				return
			}

			require.NoError(t, err)

			expectedPath := filepath.Join(toolsDir, tt.target.Name, tt.target.Version, tt.target.BinaryName)
			assert.Equal(t, expectedPath, result.BinaryPath)

			// Verify binary exists and is executable
			info, err := os.Stat(result.BinaryPath)
			require.NoError(t, err)
			assert.NotEqual(t, os.FileMode(0), info.Mode()&0111, "expected executable permission")
		})
	}
}

func TestEnsureExecutable(t *testing.T) {
	t.Parallel()

	t.Run("adds exec bit to non-executable file and preserves content", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "tool")
		content := []byte("binary content")
		require.NoError(t, os.WriteFile(path, content, 0o644))

		require.NoError(t, EnsureExecutable(path))

		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.NotEqual(t, os.FileMode(0), info.Mode()&0o111, "expected executable permission")

		got, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, content, got, "content must be unchanged")
	})

	t.Run("preserves existing read/write bits (0640 -> 0751)", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "tool")
		require.NoError(t, os.WriteFile(path, []byte("x"), 0o640))

		require.NoError(t, EnsureExecutable(path))

		// mode|0o111 adds exec for all classes while preserving r/w bits
		// (0640 -> 0751), unlike a blunt chmod 0755. os.Chmod is not subject to umask.
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o751), info.Mode().Perm())
	})

	t.Run("idempotent no-op when already executable", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "tool")
		require.NoError(t, os.WriteFile(path, []byte("x"), 0o755))

		require.NoError(t, EnsureExecutable(path))

		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o755), info.Mode().Perm(), "mode must be unchanged")
	})

	t.Run("returns error when path does not exist", func(t *testing.T) {
		t.Parallel()
		err := EnsureExecutable(filepath.Join(t.TempDir(), "missing"))
		require.Error(t, err)
	})
}

func TestPlacer_Symlink(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		setup      func(t *testing.T, toolsDir string, target Target)
		target     Target
		wantErr    bool
		errContain string
	}{
		{
			name: "create symlink",
			setup: func(t *testing.T, toolsDir string, target Target) {
				binPath := filepath.Join(toolsDir, target.Name, target.Version, target.BinaryName)
				err := os.MkdirAll(filepath.Dir(binPath), 0755)
				require.NoError(t, err)
				err = os.WriteFile(binPath, []byte("binary"), 0755)
				require.NoError(t, err)
			},
			target: Target{
				Name:       "mytool",
				Version:    "1.0.0",
				BinaryName: "tool",
			},
			wantErr: false,
		},
		{
			name: "overwrite existing symlink",
			setup: func(t *testing.T, toolsDir string, target Target) {
				binPath := filepath.Join(toolsDir, target.Name, target.Version, target.BinaryName)
				err := os.MkdirAll(filepath.Dir(binPath), 0755)
				require.NoError(t, err)
				err = os.WriteFile(binPath, []byte("binary"), 0755)
				require.NoError(t, err)
			},
			target: Target{
				Name:       "mytool",
				Version:    "1.0.0",
				BinaryName: "tool",
			},
			wantErr: false,
		},
		{
			name: "source binary not found",
			setup: func(t *testing.T, toolsDir string, target Target) {
				// Don't create the binary
			},
			target: Target{
				Name:       "mytool",
				Version:    "1.0.0",
				BinaryName: "tool",
			},
			wantErr:    true,
			errContain: "source binary not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			toolsDir := filepath.Join(tmpDir, "tools")
			binDir := filepath.Join(tmpDir, "bin")

			tt.setup(t, toolsDir, tt.target)

			// For "overwrite existing symlink" test, create an existing symlink
			if tt.name == "overwrite existing symlink" {
				err := os.MkdirAll(binDir, 0755)
				require.NoError(t, err)
				oldTarget := filepath.Join(tmpDir, "old_tool")
				err = os.WriteFile(oldTarget, []byte("old binary"), 0755)
				require.NoError(t, err)
				linkPath := filepath.Join(binDir, tt.target.BinaryName)
				err = os.Symlink(oldTarget, linkPath)
				require.NoError(t, err)
			}

			p := NewPlacer(toolsDir)
			linkPath, err := p.Symlink(tt.target, binDir)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContain != "" {
					assert.Contains(t, err.Error(), tt.errContain)
				}
				return
			}

			require.NoError(t, err)

			expectedLinkPath := filepath.Join(binDir, tt.target.BinaryName)
			assert.Equal(t, expectedLinkPath, linkPath)

			// Verify symlink points to correct target
			expectedTarget := filepath.Join(toolsDir, tt.target.Name, tt.target.Version, tt.target.BinaryName)
			actualTarget, err := os.Readlink(linkPath)
			require.NoError(t, err)
			assert.Equal(t, expectedTarget, actualTarget)
		})
	}
}

func TestPlacer_Symlink_RoutesToProvidedBinDir(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	userBinDir := filepath.Join(tmpDir, "userbin")
	sysBinDir := filepath.Join(tmpDir, "sysbin")
	target := Target{Name: "mytool", Version: "1.0.0", BinaryName: "tool"}

	binPath := filepath.Join(toolsDir, target.Name, target.Version, target.BinaryName)
	require.NoError(t, os.MkdirAll(filepath.Dir(binPath), 0755))
	require.NoError(t, os.WriteFile(binPath, []byte("binary"), 0755))

	p := NewPlacer(toolsDir)

	gotUser, err := p.Symlink(target, userBinDir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(userBinDir, target.BinaryName), gotUser)

	gotSys, err := p.Symlink(target, sysBinDir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(sysBinDir, target.BinaryName), gotSys)

	// Both links must coexist — proves the placer carries no hidden bin-dir state.
	userTarget, err := os.Readlink(gotUser)
	require.NoError(t, err)
	assert.Equal(t, binPath, userTarget)
	sysTarget, err := os.Readlink(gotSys)
	require.NoError(t, err)
	assert.Equal(t, binPath, sysTarget)
}

func TestPlacer_LinkPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		binDir string
		target Target
		want   string
	}{
		{
			name:   "user binDir",
			binDir: "/home/user/.local/bin",
			target: Target{Name: "ripgrep", Version: "14.1.1", BinaryName: "rg"},
			want:   "/home/user/.local/bin/rg",
		},
		{
			name:   "system binDir",
			binDir: "/usr/local/bin",
			target: Target{Name: "kubectl", Version: "1.30.0", BinaryName: "kubectl"},
			want:   "/usr/local/bin/kubectl",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := NewPlacer("/tools")
			assert.Equal(t, tt.want, p.LinkPath(tt.target, tt.binDir))
		})
	}
}

func TestPlacer_Symlink_RejectsBadBinDir(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	target := Target{Name: "mytool", Version: "1.0.0", BinaryName: "tool"}

	binPath := filepath.Join(toolsDir, target.Name, target.Version, target.BinaryName)
	require.NoError(t, os.MkdirAll(filepath.Dir(binPath), 0755))
	require.NoError(t, os.WriteFile(binPath, []byte("binary"), 0755))

	tests := []struct {
		name    string
		binDir  string
		wantMsg string
	}{
		{"empty", "", "is empty"},
		{"relative", "relative/bin", "is not absolute"},
		{"root", "/", "resolves to root"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := NewPlacer(toolsDir)
			_, err := p.Symlink(target, tt.binDir)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestPlacer_Cleanup(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		setup   func(t *testing.T, tmpDir string) string
		wantErr bool
	}{
		{
			name: "cleanup directory",
			setup: func(t *testing.T, tmpDir string) string {
				dir := filepath.Join(tmpDir, "to_remove")
				err := os.MkdirAll(dir, 0755)
				require.NoError(t, err)
				err = os.WriteFile(filepath.Join(dir, "file"), []byte("content"), 0644)
				require.NoError(t, err)
				return dir
			},
			wantErr: false,
		},
		{
			name: "cleanup file",
			setup: func(t *testing.T, tmpDir string) string {
				file := filepath.Join(tmpDir, "to_remove.tar.gz")
				err := os.WriteFile(file, []byte("content"), 0644)
				require.NoError(t, err)
				return file
			},
			wantErr: false,
		},
		{
			name: "cleanup nonexistent path - no error",
			setup: func(t *testing.T, tmpDir string) string {
				return filepath.Join(tmpDir, "nonexistent")
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			path := tt.setup(t, tmpDir)

			p := NewPlacer(filepath.Join(tmpDir, "tools"))
			err := p.Cleanup(path)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Verify path no longer exists
			_, err = os.Stat(path)
			assert.True(t, os.IsNotExist(err))
		})
	}
}

// Helper function
func sha256Hash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
