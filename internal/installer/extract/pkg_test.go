package extract

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPkgExtractor_RequiresOSFile(t *testing.T) {
	t.Parallel()

	extractor, err := NewExtractor(ArchiveTypePkg)
	require.NoError(t, err)

	// Pass a non-*os.File reader — should fail with a clear error
	r := bytes.NewReader([]byte("dummy"))
	err = extractor.Extract(r, t.TempDir())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "*os.File")
}

func TestValidateExtractedPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		setup      func(t *testing.T, dir string)
		wantErr    bool
		errContain string
	}{
		{
			name: "valid symlink inside destDir",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "target.txt"), []byte("data"), 0644))
				require.NoError(t, os.Symlink("target.txt", filepath.Join(dir, "link.txt")))
			},
			wantErr: false,
		},
		{
			name: "valid symlink in subdirectory",
			setup: func(t *testing.T, dir string) {
				sub := filepath.Join(dir, "sub")
				require.NoError(t, os.MkdirAll(sub, 0755))
				require.NoError(t, os.WriteFile(filepath.Join(sub, "real.txt"), []byte("data"), 0644))
				require.NoError(t, os.Symlink("real.txt", filepath.Join(sub, "link.txt")))
			},
			wantErr: false,
		},
		{
			name: "symlink escaping destDir",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.Symlink("../../etc/passwd", filepath.Join(dir, "escape")))
			},
			wantErr:    true,
			errContain: "invalid symlink target in pkg",
		},
		{
			name: "deep relative symlink escaping destDir",
			setup: func(t *testing.T, dir string) {
				sub := filepath.Join(dir, "a", "b", "c")
				require.NoError(t, os.MkdirAll(sub, 0755))
				require.NoError(t, os.Symlink("../../../../../../../../etc/passwd", filepath.Join(sub, "escape")))
			},
			wantErr:    true,
			errContain: "invalid symlink target in pkg",
		},
		{
			name: "no symlinks at all",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("ok"), 0644))
				require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0755))
			},
			wantErr: false,
		},
		{
			name: "empty directory",
			setup: func(t *testing.T, dir string) {
				// dir is already created by t.TempDir()
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			tt.setup(t, dir)

			err := validateExtractedPaths(dir)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContain != "" {
					assert.Contains(t, err.Error(), tt.errContain)
				}
				return
			}
			require.NoError(t, err)
		})
	}
}
