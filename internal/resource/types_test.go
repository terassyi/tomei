package resource

import (
	"testing"
	"time"
)

func TestToolSpec_IsEnabled(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		enabled *bool
		want    bool
	}{
		{"nil (default true)", nil, true},
		{"explicit true", ptr(true), true},
		{"explicit false", ptr(false), false},
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
		{"explicit true", ptr(true), true},
		{"explicit false", ptr(false), false},
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

	state.Taint("runtime_upgraded")
	if !state.IsTainted() {
		t.Error("state should be tainted after Taint()")
	}
	if state.TaintReason != "runtime_upgraded" {
		t.Errorf("expected taint reason 'runtime_upgraded', got %q", state.TaintReason)
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

func ptr[T any](v T) *T {
	return &v
}
