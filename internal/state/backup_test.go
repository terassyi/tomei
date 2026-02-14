package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/resource"
)

func TestBackupPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		statePath string
		want      string
	}{
		{
			name:      "standard path",
			statePath: "/home/user/.local/share/tomei/state.json",
			want:      "/home/user/.local/share/tomei/state.json.bak",
		},
		{
			name:      "relative path",
			statePath: "state.json",
			want:      "state.json.bak",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, BackupPath(tt.statePath))
		})
	}
}

func TestCreateBackup(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		setup     func(t *testing.T, dir string)
		wantErr   bool
		wantExist bool
	}{
		{
			name: "creates backup from existing state",
			setup: func(t *testing.T, dir string) {
				st := &UserState{
					Version: Version,
					Tools: map[string]*resource.ToolState{
						"gh": {Version: "2.86.0", UpdatedAt: time.Now()},
					},
				}
				store, err := NewStore[UserState](dir)
				require.NoError(t, err)
				require.NoError(t, store.Lock())
				require.NoError(t, store.Save(st))
				require.NoError(t, store.Unlock())
			},
			wantExist: true,
		},
		{
			name:      "no error when state file does not exist",
			setup:     func(t *testing.T, dir string) {},
			wantExist: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			tt.setup(t, dir)

			store, err := NewStore[UserState](dir)
			require.NoError(t, err)
			require.NoError(t, store.Lock())
			defer func() { _ = store.Unlock() }()

			err = CreateBackup(store)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			bakPath := BackupPath(store.StatePath())
			if tt.wantExist {
				assert.FileExists(t, bakPath)

				// Verify backup content matches original
				original, err := os.ReadFile(store.StatePath())
				require.NoError(t, err)
				backup, err := os.ReadFile(bakPath)
				require.NoError(t, err)
				assert.Equal(t, original, backup)
			} else {
				_, err := os.Stat(bakPath)
				assert.True(t, os.IsNotExist(err))
			}
		})
	}
}

func TestCreateBackup_AtomicWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := NewStore[UserState](dir)
	require.NoError(t, err)

	// Create a state file
	require.NoError(t, store.Lock())
	st := &UserState{Version: Version}
	require.NoError(t, store.Save(st))

	require.NoError(t, CreateBackup(store))
	_ = store.Unlock()

	// Temp file should not exist after successful backup
	tmpPath := BackupPath(store.StatePath()) + ".tmp"
	_, err = os.Stat(tmpPath)
	assert.True(t, os.IsNotExist(err), "temp file should not exist after successful backup")
}

func TestLoadBackup(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		setup   func(t *testing.T, dir string) string
		wantNil bool
		wantErr bool
		check   func(t *testing.T, st *UserState)
	}{
		{
			name: "loads existing backup",
			setup: func(t *testing.T, dir string) string {
				store, err := NewStore[UserState](dir)
				require.NoError(t, err)
				require.NoError(t, store.Lock())
				st := &UserState{
					Version: Version,
					Tools: map[string]*resource.ToolState{
						"ripgrep": {Version: "14.0.0", UpdatedAt: time.Now()},
					},
				}
				require.NoError(t, store.Save(st))
				require.NoError(t, CreateBackup(store))
				_ = store.Unlock()
				return store.StatePath()
			},
			check: func(t *testing.T, st *UserState) {
				assert.Equal(t, Version, st.Version)
				require.Contains(t, st.Tools, "ripgrep")
				assert.Equal(t, "14.0.0", st.Tools["ripgrep"].Version)
			},
		},
		{
			name: "returns nil when backup does not exist",
			setup: func(t *testing.T, dir string) string {
				return filepath.Join(dir, "state.json")
			},
			wantNil: true,
		},
		{
			name: "error on corrupted backup",
			setup: func(t *testing.T, dir string) string {
				statePath := filepath.Join(dir, "state.json")
				bakPath := BackupPath(statePath)
				require.NoError(t, os.WriteFile(bakPath, []byte("invalid json{"), 0644))
				return statePath
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			statePath := tt.setup(t, dir)

			st, err := LoadBackup[UserState](statePath)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			if tt.wantNil {
				assert.Nil(t, st)
				return
			}

			require.NotNil(t, st)
			tt.check(t, st)
		})
	}
}
