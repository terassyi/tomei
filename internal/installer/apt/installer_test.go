package apt

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/installer/command"
	"github.com/terassyi/tomei/internal/installer/executor"
	"github.com/terassyi/tomei/internal/resource"
)

// --- mock ---

type captureCall struct {
	cmds       []string
	vars       command.Vars
	env        map[string]string
	privileged bool
}

type mockCommandRunner struct {
	captureOutput string
	captureErr    error
	calls         []captureCall
}

func (m *mockCommandRunner) ExecuteCapture(ctx context.Context, cmds []string, vars command.Vars, env map[string]string) (string, error) {
	m.calls = append(m.calls, captureCall{
		cmds:       cmds,
		vars:       vars,
		env:        env,
		privileged: executor.PrivilegedFromContext(ctx),
	})
	return m.captureOutput, m.captureErr
}

// --- InstallerInstaller tests ---

func TestInstallerInstaller_Install(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		captureOutput string
		captureErr    error
		wantVersion   string
		wantErr       bool
		wantErrMsg    string
	}{
		{
			name:          "successful version detection",
			captureOutput: "apt 2.4.12 (amd64)",
			wantVersion:   "2.4.12",
		},
		{
			name:          "version with build suffix",
			captureOutput: "apt 2.7.14build2 (amd64)",
			wantVersion:   "2.7.14build2",
		},
		{
			name:       "apt-get not found",
			captureErr: fmt.Errorf("exec: \"apt-get\": executable file not found in $PATH"),
			wantErr:    true,
			wantErrMsg: "failed to run apt-get --version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := &mockCommandRunner{
				captureOutput: tt.captureOutput,
				captureErr:    tt.captureErr,
			}
			inst := newInstallerInstallerWith(mock)

			res := &resource.SystemInstaller{
				BaseResource: resource.BaseResource{
					Metadata: resource.Metadata{Name: "apt"},
				},
				SystemInstallerSpec: &resource.SystemInstallerSpec{
					Pattern:    "delegation",
					Privileged: true,
				},
			}

			state, err := inst.Install(context.Background(), res, "apt")

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrMsg)
				return
			}
			require.NoError(t, err)

			assert.Equal(t, tt.wantVersion, state.Version)
			assert.False(t, state.UpdatedAt.IsZero())

			// Verify command arguments
			require.Len(t, mock.calls, 1)
			call := mock.calls[0]
			assert.Equal(t, []string{"apt-get --version"}, call.cmds)
			assert.Equal(t, command.Vars{}, call.vars)
			assert.Nil(t, call.env)
			// apt-get --version does not require sudo
			assert.False(t, call.privileged)
		})
	}
}

func TestInstallerInstaller_Remove(t *testing.T) {
	t.Parallel()
	mock := &mockCommandRunner{}
	inst := newInstallerInstallerWith(mock)

	st := &resource.SystemInstallerState{
		Version: "2.4.12",
	}

	err := inst.Remove(context.Background(), st, "apt")
	require.NoError(t, err)

	// Remove is a no-op — no commands should be executed
	assert.Empty(t, mock.calls)
}

// Compile-time interface satisfaction check.
var _ executor.Installer[*resource.SystemInstaller, *resource.SystemInstallerState] = (*InstallerInstaller)(nil)
