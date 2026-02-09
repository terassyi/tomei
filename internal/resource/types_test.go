package resource

import (
	"testing"
	"time"
)

func TestToolSpec_IsEnabled(t *testing.T) {
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
			spec := &ToolSpec{Enabled: tt.enabled}
			if got := spec.IsEnabled(); got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToolItem_IsEnabled(t *testing.T) {
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
			item := &ToolItem{Enabled: tt.enabled}
			if got := item.IsEnabled(); got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToolState_Taint(t *testing.T) {
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
			action := &Action{ActionType: tt.actionType}
			if got := action.NeedsExecution(); got != tt.want {
				t.Errorf("NeedsExecution() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseRef(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Ref
		wantErr bool
	}{
		{
			name:  "tool/ripgrep",
			input: "tool/ripgrep",
			want:  Ref{Kind: "tool", Name: "ripgrep"},
		},
		{
			name:  "Runtime/go",
			input: "Runtime/go",
			want:  Ref{Kind: "Runtime", Name: "go"},
		},
		{
			name:  "name with slash",
			input: "tool/foo/bar",
			want:  Ref{Kind: "tool", Name: "foo/bar"},
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

func ptr[T any](v T) *T {
	return &v
}
