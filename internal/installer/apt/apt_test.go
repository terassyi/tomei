package apt

import (
	"context"
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
	captureOutput string
	captureErr    error
}

var _ CommandRunner = (*mockCommandRunner)(nil)

func (m *mockCommandRunner) ExecuteCapture(_ context.Context, _ []string, _ command.Vars, _ map[string]string) (string, error) {
	return m.captureOutput, m.captureErr
}

// --- VersionFunc tests ---

func TestVersionFunc_Success(t *testing.T) {
	t.Parallel()
	mock := &mockCommandRunner{captureOutput: "apt 2.7.14build2 (amd64)"}
	vf := VersionFunc(mock)

	version, err := vf(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "2.7.14build2", version)
}

func TestVersionFunc_CommandError(t *testing.T) {
	t.Parallel()
	mock := &mockCommandRunner{captureErr: fmt.Errorf("exec: \"apt-get\": executable file not found in $PATH")}
	vf := VersionFunc(mock)

	_, err := vf(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to run apt-get --version")
}

func TestVersionFunc_ParseError(t *testing.T) {
	t.Parallel()
	mock := &mockCommandRunner{captureOutput: "apt"}
	vf := VersionFunc(mock)

	_, err := vf(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected apt-get --version output")
}
