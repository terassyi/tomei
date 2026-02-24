package resource

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolSpec_IsEnabled(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		enabled *bool
		want    bool
	}{
		{"nil (default true)", nil, true},
		{"explicit true", new(true), true},
		{"explicit false", new(false), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec := &ToolSpec{Enabled: tt.enabled}
			if got := spec.IsEnabled(); got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToolItem_IsEnabled(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		enabled *bool
		want    bool
	}{
		{"nil (default true)", nil, true},
		{"explicit true", new(true), true},
		{"explicit false", new(false), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			item := &ToolItem{Enabled: tt.enabled}
			if got := item.IsEnabled(); got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToolState_Taint(t *testing.T) {
	t.Parallel()
	state := &ToolState{
		InstallerRef: "go",
		Version:      "0.16.0",
		UpdatedAt:    time.Now(),
	}

	if state.IsTainted() {
		t.Error("new state should not be tainted")
	}

	state.Taint(TaintReasonRuntimeUpgraded)
	if !state.IsTainted() {
		t.Error("state should be tainted after Taint()")
	}
	if state.TaintReason != TaintReasonRuntimeUpgraded {
		t.Errorf("expected taint reason %q, got %q", TaintReasonRuntimeUpgraded, state.TaintReason)
	}

	state.ClearTaint()
	if state.IsTainted() {
		t.Error("state should not be tainted after ClearTaint()")
	}
}

func TestAction_NeedsExecution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		actionType ActionType
		want       bool
	}{
		{"none", ActionNone, false},
		{"install", ActionInstall, true},
		{"upgrade", ActionUpgrade, true},
		{"downgrade", ActionDowngrade, true},
		{"reinstall", ActionReinstall, true},
		{"remove", ActionRemove, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			action := &Action{ActionType: tt.actionType}
			if got := action.NeedsExecution(); got != tt.want {
				t.Errorf("NeedsExecution() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalizeKind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  Kind
		ok    bool
	}{
		{"lowercase tool", "tool", KindTool, true},
		{"PascalCase Tool", "Tool", KindTool, true},
		{"uppercase TOOL", "TOOL", KindTool, true},
		{"lowercase runtime", "runtime", KindRuntime, true},
		{"PascalCase Runtime", "Runtime", KindRuntime, true},
		{"lowercase installerrepository", "installerrepository", KindInstallerRepository, true},
		{"PascalCase InstallerRepository", "InstallerRepository", KindInstallerRepository, true},
		{"lowercase toolset", "toolset", KindToolSet, true},
		{"PascalCase ToolSet", "ToolSet", KindToolSet, true},
		{"lowercase installer", "installer", KindInstaller, true},
		{"lowercase systeminstaller", "systeminstaller", KindSystemInstaller, true},
		{"lowercase systempackagerepository", "systempackagerepository", KindSystemPackageRepository, true},
		{"lowercase systempackageset", "systempackageset", KindSystemPackageSet, true},
		{"unknown kind", "unknown", Kind(""), false},
		{"empty string", "", Kind(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := NormalizeKind(tt.input)
			if ok != tt.ok {
				t.Errorf("NormalizeKind(%q) ok = %v, want %v", tt.input, ok, tt.ok)
			}
			if got != tt.want {
				t.Errorf("NormalizeKind(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseRef(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    Ref
		wantErr bool
	}{
		{
			name:  "lowercase tool/ripgrep",
			input: "tool/ripgrep",
			want:  Ref{Kind: KindTool, Name: "ripgrep"},
		},
		{
			name:  "PascalCase Tool/ripgrep",
			input: "Tool/ripgrep",
			want:  Ref{Kind: KindTool, Name: "ripgrep"},
		},
		{
			name:  "Runtime/go",
			input: "Runtime/go",
			want:  Ref{Kind: KindRuntime, Name: "go"},
		},
		{
			name:  "lowercase runtime/go",
			input: "runtime/go",
			want:  Ref{Kind: KindRuntime, Name: "go"},
		},
		{
			name:  "name with slash",
			input: "tool/foo/bar",
			want:  Ref{Kind: KindTool, Name: "foo/bar"},
		},
		{
			name:    "no slash",
			input:   "ripgrep",
			wantErr: true,
		},
		{
			name:    "empty kind",
			input:   "/ripgrep",
			wantErr: true,
		},
		{
			name:    "empty name",
			input:   "tool/",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "unknown kind",
			input:   "unknown/foo",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseRef(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseRef(%q) expected error, got %v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseRef(%q) unexpected error: %v", tt.input, err)
				return
			}
			if got != tt.want {
				t.Errorf("ParseRef(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseRefArgs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    []string
		want    Ref
		wantErr bool
	}{
		{
			name: "slash format lowercase",
			args: []string{"tool/ripgrep"},
			want: Ref{Kind: KindTool, Name: "ripgrep"},
		},
		{
			name: "slash format PascalCase",
			args: []string{"Tool/ripgrep"},
			want: Ref{Kind: KindTool, Name: "ripgrep"},
		},
		{
			name: "two args lowercase",
			args: []string{"tool", "ripgrep"},
			want: Ref{Kind: KindTool, Name: "ripgrep"},
		},
		{
			name: "two args PascalCase",
			args: []string{"Tool", "ripgrep"},
			want: Ref{Kind: KindTool, Name: "ripgrep"},
		},
		{
			name: "two args runtime",
			args: []string{"runtime", "go"},
			want: Ref{Kind: KindRuntime, Name: "go"},
		},
		{
			name:    "zero args",
			args:    []string{},
			wantErr: true,
		},
		{
			name:    "three args",
			args:    []string{"tool", "ripgrep", "extra"},
			wantErr: true,
		},
		{
			name:    "one arg no slash",
			args:    []string{"ripgrep"},
			wantErr: true,
		},
		{
			name:    "two args unknown kind",
			args:    []string{"unknown", "foo"},
			wantErr: true,
		},
		{
			name:    "two args empty name",
			args:    []string{"tool", ""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseRefArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseRefArgs(%v) expected error, got %v", tt.args, got)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseRefArgs(%v) unexpected error: %v", tt.args, err)
				return
			}
			if got != tt.want {
				t.Errorf("ParseRefArgs(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func Test_unmarshalStringOrSlice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   json.RawMessage
		want    []string
		wantErr bool
	}{
		{
			name:  "null",
			input: json.RawMessage(`null`),
			want:  nil,
		},
		{
			name:  "empty input",
			input: nil,
			want:  nil,
		},
		{
			name:  "bare string",
			input: json.RawMessage(`"hello"`),
			want:  []string{"hello"},
		},
		{
			name:  "single-element array",
			input: json.RawMessage(`["hello"]`),
			want:  []string{"hello"},
		},
		{
			name:  "multi-element array",
			input: json.RawMessage(`["a","b","c"]`),
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "empty array",
			input: json.RawMessage(`[]`),
			want:  []string{},
		},
		{
			name:    "invalid JSON",
			input:   json.RawMessage(`{bad`),
			wantErr: true,
		},
		{
			name:    "number",
			input:   json.RawMessage(`42`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := unmarshalStringOrSlice(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsLatestVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{"empty string", "", true},
		{"latest", "latest", true},
		{"stable", "stable", false},
		{"exact version", "1.0.0", false},
		{"lts", "lts", false},
		{"LATEST uppercase", "LATEST", false},
		{"Latest mixed case", "Latest", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsLatestVersion(tt.version)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestClassifyVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		version string
		want    VersionKind
	}{
		{"empty string", "", VersionLatest},
		{"latest string", "latest", VersionLatest},
		{"exact version", "1.0.0", VersionExact},
		{"stable alias", "stable", VersionExact},
		{"v-prefixed version", "v2.1.0", VersionExact},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyVersion(tt.version)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsExactVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		version string
		want    bool
	}{
		// Exact versions
		{"exact semver", "1.26.0", true},
		{"exact with v prefix", "v2.1.4", true},
		{"exact minor", "0.10.2", true},
		{"exact major only", "3", true},
		{"exact zero version", "0.0.1", true},
		{"exact with pre-release", "1.0.0-beta1", true},
		{"exact with build metadata", "1.0.0+build.123", true},
		{"exact v0", "v0", true},
		{"exact two-part version", "1.25", true},
		{"exact long version", "12.345.6789", true},

		// Aliases / non-exact
		{"alias latest", "latest", false},
		{"alias stable", "stable", false},
		{"alias lts", "lts", false},
		{"alias nightly", "nightly", false},
		{"alias beta", "beta", false},
		{"empty string", "", false},
		{"v prefix only", "v", false},
		{"uppercase alias", "LATEST", false},
		{"alias with hyphen", "release-candidate", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsExactVersion(tt.version)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestToolCommandSet_UnmarshalJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		json    string
		want    ToolCommandSet
		wantErr bool
	}{
		{
			name: "all fields as arrays",
			json: `{"install":["curl -fsSL https://example.com/install.sh | sh"],"update":["tool update"],"check":["tool --version"],"remove":["tool uninstall"],"resolveVersion":["tool --version | grep -oP '\\d+\\.\\d+\\.\\d+'"]}`,
			want: ToolCommandSet{
				CommandSet: CommandSet{
					Install: []string{"curl -fsSL https://example.com/install.sh | sh"},
					Check:   []string{"tool --version"},
					Remove:  []string{"tool uninstall"},
				},
				Update:         []string{"tool update"},
				ResolveVersion: []string{`tool --version | grep -oP '\d+\.\d+\.\d+'`},
			},
		},
		{
			name: "all fields as bare strings (CUE single-element list)",
			json: `{"install":"curl -fsSL https://example.com/install.sh | sh","update":"tool update","check":"tool --version","remove":"tool uninstall","resolveVersion":"tool --version"}`,
			want: ToolCommandSet{
				CommandSet: CommandSet{
					Install: []string{"curl -fsSL https://example.com/install.sh | sh"},
					Check:   []string{"tool --version"},
					Remove:  []string{"tool uninstall"},
				},
				Update:         []string{"tool update"},
				ResolveVersion: []string{"tool --version"},
			},
		},
		{
			name: "install only (minimal)",
			json: `{"install":"cmd"}`,
			want: ToolCommandSet{
				CommandSet: CommandSet{
					Install: []string{"cmd"},
				},
			},
		},
		{
			name: "install and resolveVersion only",
			json: `{"install":["cmd"],"resolveVersion":["github-release:owner/repo:v"]}`,
			want: ToolCommandSet{
				CommandSet: CommandSet{
					Install: []string{"cmd"},
				},
				ResolveVersion: []string{"github-release:owner/repo:v"},
			},
		},
		{
			name: "mixed bare string and array",
			json: `{"install":"cmd1","update":["up1","up2"],"check":"chk"}`,
			want: ToolCommandSet{
				CommandSet: CommandSet{
					Install: []string{"cmd1"},
					Check:   []string{"chk"},
				},
				Update: []string{"up1", "up2"},
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
			var got ToolCommandSet
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

func TestCommandSet_UnmarshalJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		json    string
		want    CommandSet
		wantErr bool
	}{
		{
			name: "all fields as arrays",
			json: `{"install":["cmd1","cmd2"],"check":["check1"],"remove":["rm1"]}`,
			want: CommandSet{
				Install: []string{"cmd1", "cmd2"},
				Check:   []string{"check1"},
				Remove:  []string{"rm1"},
			},
		},
		{
			name: "all fields as bare strings",
			json: `{"install":"cmd1","check":"check1","remove":"rm1"}`,
			want: CommandSet{
				Install: []string{"cmd1"},
				Check:   []string{"check1"},
				Remove:  []string{"rm1"},
			},
		},
		{
			name: "install only",
			json: `{"install":"cmd1"}`,
			want: CommandSet{
				Install: []string{"cmd1"},
			},
		},
		{
			name: "mixed bare string and array",
			json: `{"install":"cmd1","check":["a","b"]}`,
			want: CommandSet{
				Install: []string{"cmd1"},
				Check:   []string{"a", "b"},
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
			var got CommandSet
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
