package resource

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuntimeSpec_UnmarshalJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		json    string
		want    RuntimeSpec
		wantErr bool
	}{
		{
			name: "binaries as array",
			json: `{"type":"download","version":"1.25.6","toolBinPath":"~/go/bin","binaries":["go","gofmt"]}`,
			want: RuntimeSpec{
				Type:        InstallTypeDownload,
				Version:     "1.25.6",
				ToolBinPath: "~/go/bin",
				Binaries:    []string{"go", "gofmt"},
			},
		},
		{
			name: "binaries as bare string",
			json: `{"type":"download","version":"1.25.6","toolBinPath":"~/go/bin","binaries":"go"}`,
			want: RuntimeSpec{
				Type:        InstallTypeDownload,
				Version:     "1.25.6",
				ToolBinPath: "~/go/bin",
				Binaries:    []string{"go"},
			},
		},
		{
			name: "no binaries field",
			json: `{"type":"download","version":"1.25.6","toolBinPath":"~/go/bin"}`,
			want: RuntimeSpec{
				Type:        InstallTypeDownload,
				Version:     "1.25.6",
				ToolBinPath: "~/go/bin",
			},
		},
		{
			name: "other fields preserved",
			json: `{"type":"delegation","version":"stable","toolBinPath":"~/.cargo/bin","taintOnUpgrade":true}`,
			want: RuntimeSpec{
				Type:           InstallTypeDelegation,
				Version:        "stable",
				ToolBinPath:    "~/.cargo/bin",
				TaintOnUpgrade: true,
			},
		},
		{
			name:    "invalid JSON",
			json:    `{bad}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got RuntimeSpec
			err := got.UnmarshalJSON([]byte(tt.json))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRuntimeBootstrapSpec_UnmarshalJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		json    string
		want    RuntimeBootstrapSpec
		wantErr bool
	}{
		{
			name: "all fields as bare strings",
			json: `{"install":"curl -sSf https://sh.rustup.rs | sh","check":"rustc --version","remove":"rustup self uninstall","resolveVersion":"rustup show active-toolchain"}`,
			want: RuntimeBootstrapSpec{
				CommandSet: CommandSet{
					Install: []string{"curl -sSf https://sh.rustup.rs | sh"},
					Check:   []string{"rustc --version"},
					Remove:  []string{"rustup self uninstall"},
				},
				ResolveVersion: []string{"rustup show active-toolchain"},
			},
		},
		{
			name: "all fields as arrays",
			json: `{"install":["cmd1","cmd2"],"check":["check1"],"remove":["rm1"],"resolveVersion":["resolve1","resolve2"]}`,
			want: RuntimeBootstrapSpec{
				CommandSet: CommandSet{
					Install: []string{"cmd1", "cmd2"},
					Check:   []string{"check1"},
					Remove:  []string{"rm1"},
				},
				ResolveVersion: []string{"resolve1", "resolve2"},
			},
		},
		{
			name: "without resolveVersion",
			json: `{"install":"cmd1","check":"check1"}`,
			want: RuntimeBootstrapSpec{
				CommandSet: CommandSet{
					Install: []string{"cmd1"},
					Check:   []string{"check1"},
				},
			},
		},
		{
			name:    "invalid JSON",
			json:    `{bad}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got RuntimeBootstrapSpec
			err := got.UnmarshalJSON([]byte(tt.json))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
