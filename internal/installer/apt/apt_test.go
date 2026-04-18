package apt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
