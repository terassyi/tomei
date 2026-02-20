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
			name: "resolveVersion as array",
			json: `{"type":"download","version":"latest","toolBinPath":"~/bin","resolveVersion":["curl -sL https://go.dev/VERSION"]}`,
			want: RuntimeSpec{
				Type:           InstallTypeDownload,
				Version:        "latest",
				ToolBinPath:    "~/bin",
				ResolveVersion: []string{"curl -sL https://go.dev/VERSION"},
			},
		},
		{
			name: "resolveVersion as bare string",
			json: `{"type":"download","version":"latest","toolBinPath":"~/bin","resolveVersion":"github-release:oven-sh/bun:bun-v"}`,
			want: RuntimeSpec{
				Type:           InstallTypeDownload,
				Version:        "latest",
				ToolBinPath:    "~/bin",
				ResolveVersion: []string{"github-release:oven-sh/bun:bun-v"},
			},
		},
		{
			name: "resolveVersion with binaries",
			json: `{"type":"download","version":"latest","toolBinPath":"~/bin","binaries":["go","gofmt"],"resolveVersion":["echo 1.25.6"]}`,
			want: RuntimeSpec{
				Type:           InstallTypeDownload,
				Version:        "latest",
				ToolBinPath:    "~/bin",
				Binaries:       []string{"go", "gofmt"},
				ResolveVersion: []string{"echo 1.25.6"},
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

func TestRuntimeState_Taint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		initial      *RuntimeState
		taintReason  TaintReason
		wantTainted  bool
		wantReason   TaintReason
		clearTaint   bool
		wantAfterClr bool
	}{
		{
			name:        "taint empty state",
			initial:     &RuntimeState{},
			taintReason: TaintReasonUpdateRequested,
			wantTainted: true,
			wantReason:  TaintReasonUpdateRequested,
		},
		{
			name:        "taint with runtime_upgraded reason",
			initial:     &RuntimeState{Version: "1.83.0"},
			taintReason: TaintReasonRuntimeUpgraded,
			wantTainted: true,
			wantReason:  TaintReasonRuntimeUpgraded,
		},
		{
			name:         "taint then clear",
			initial:      &RuntimeState{},
			taintReason:  TaintReasonUpdateRequested,
			wantTainted:  true,
			wantReason:   TaintReasonUpdateRequested,
			clearTaint:   true,
			wantAfterClr: false,
		},
		{
			name:        "untainted state is not tainted",
			initial:     &RuntimeState{Version: "1.25.6"},
			wantTainted: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := tt.initial

			if tt.taintReason != "" {
				s.Taint(tt.taintReason)
			}

			assert.Equal(t, tt.wantTainted, s.IsTainted())
			if tt.wantReason != "" {
				assert.Equal(t, tt.wantReason, s.TaintReason)
			}

			if tt.clearTaint {
				s.ClearTaint()
				assert.Equal(t, tt.wantAfterClr, s.IsTainted())
				assert.Empty(t, s.TaintReason)
			}
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
			name: "update as bare string",
			json: `{"install":"cmd1","update":"update-cmd","check":"check1"}`,
			want: RuntimeBootstrapSpec{
				CommandSet: CommandSet{
					Install: []string{"cmd1"},
					Check:   []string{"check1"},
				},
				Update: []string{"update-cmd"},
			},
		},
		{
			name: "update as array",
			json: `{"install":["cmd1"],"update":["upd1","upd2"],"check":["check1"]}`,
			want: RuntimeBootstrapSpec{
				CommandSet: CommandSet{
					Install: []string{"cmd1"},
					Check:   []string{"check1"},
				},
				Update: []string{"upd1", "upd2"},
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
