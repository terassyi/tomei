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
	p := NewPlacer("/tools", "/bin")
	assert.NotNil(t, p)
}

func TestValidateAction_String(t *testing.T) {
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
			assert.Equal(t, tt.want, tt.action.String())
		})
	}
}

func TestPlacer_Validate(t *testing.T) {
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
			tmpDir := t.TempDir()
			toolsDir := filepath.Join(tmpDir, "tools")
			binDir := filepath.Join(tmpDir, "bin")

			tt.setup(t, toolsDir, tt.target)

			p := NewPlacer(toolsDir, binDir)
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
			tmpDir := t.TempDir()
			srcDir := filepath.Join(tmpDir, "src")
			toolsDir := filepath.Join(tmpDir, "tools")
			binDir := filepath.Join(tmpDir, "bin")

			err := os.MkdirAll(srcDir, 0755)
			require.NoError(t, err)

			tt.setup(t, srcDir)

			p := NewPlacer(toolsDir, binDir)
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

func TestPlacer_Symlink(t *testing.T) {
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

			p := NewPlacer(toolsDir, binDir)
			linkPath, err := p.Symlink(tt.target)

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

func TestPlacer_Cleanup(t *testing.T) {
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
			tmpDir := t.TempDir()
			path := tt.setup(t, tmpDir)

			p := NewPlacer(filepath.Join(tmpDir, "tools"), filepath.Join(tmpDir, "bin"))
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
