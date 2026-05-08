package apt

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/installer/command"
)

func TestParseAptVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		output  string
		want    string
		wantErr bool
	}{
		{
			name:   "standard version",
			output: "apt 2.4.12 (amd64)",
			want:   "2.4.12",
		},
		{
			name:   "version with build suffix",
			output: "apt 2.7.14build2 (amd64)",
			want:   "2.7.14build2",
		},
		{
			name:   "multiline output",
			output: "apt 2.4.12 (amd64)\nUsage: apt-get [options] command\n",
			want:   "2.4.12",
		},
		{
			name:    "empty output",
			output:  "",
			wantErr: true,
		},
		{
			name:    "single word",
			output:  "apt",
			wantErr: true,
		},
		{
			name:    "whitespace only",
			output:  "   \n  ",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseAptVersion(tt.output)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// --- mock ---

type mockCommandRunner struct {
	captureCmds   []string
	captureOutput string
	captureErr    error
}

var _ CommandRunner = (*mockCommandRunner)(nil)

func (m *mockCommandRunner) ExecuteCapture(_ context.Context, cmds []string, _ command.Vars, _ map[string]string) (string, error) {
	m.captureCmds = cmds
	return m.captureOutput, m.captureErr
}

// --- VersionFunc tests ---

func TestVersionFunc_Success(t *testing.T) {
	t.Parallel()
	mock := &mockCommandRunner{captureOutput: "apt 2.7.14build2 (amd64)"}
	vf := New(mock).VersionFunc()

	version, err := vf(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "2.7.14build2", version)
}

func TestVersionFunc_CommandError(t *testing.T) {
	t.Parallel()
	mock := &mockCommandRunner{captureErr: fmt.Errorf("exec: \"apt-get\": executable file not found in $PATH")}
	vf := New(mock).VersionFunc()

	_, err := vf(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to run apt-get --version")
}

func TestVersionFunc_ParseError(t *testing.T) {
	t.Parallel()
	mock := &mockCommandRunner{captureOutput: "apt"}
	vf := New(mock).VersionFunc()

	_, err := vf(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected apt-get --version output")
}

// --- GetInstall tests ---

func TestGetInstall(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		packages  []string
		runnerErr error
		wantErr   string
		wantCmd   string
	}{
		{
			name:     "single package",
			packages: []string{"git"},
			wantCmd:  "sudo -n apt-get install -y git",
		},
		{
			name:     "multiple packages",
			packages: []string{"git", "curl", "tree"},
			wantCmd:  "sudo -n apt-get install -y git curl tree",
		},
		{
			name:     "empty packages",
			packages: []string{},
			wantErr:  "at least one package is required",
		},
		{
			name:      "runner error wraps packages context",
			packages:  []string{"nonexistent-pkg"},
			runnerErr: errors.New("exit status 100"),
			wantErr:   "apt-get install [nonexistent-pkg]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner := &mockCommandRunner{captureErr: tt.runnerErr}
			err := New(runner).GetInstall(context.Background(), tt.packages)
			if tt.wantErr == "" {
				require.NoError(t, err)
				require.Len(t, runner.captureCmds, 1)
				assert.Equal(t, tt.wantCmd, runner.captureCmds[0])
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}
