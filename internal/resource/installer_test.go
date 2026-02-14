package resource

import (
	"testing"
)

func TestInstallerSpec_Validate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		spec    InstallerSpec
		wantErr string
	}{
		{
			name:    "empty type",
			spec:    InstallerSpec{},
			wantErr: "type is required",
		},
		{
			name: "invalid type",
			spec: InstallerSpec{
				Type: "invalid",
			},
			wantErr: "type must be 'download' or 'delegation'",
		},
		{
			name: "valid download type",
			spec: InstallerSpec{
				Type: InstallTypeDownload,
			},
			wantErr: "",
		},
		{
			name: "delegation without commands",
			spec: InstallerSpec{
				Type: InstallTypeDelegation,
			},
			wantErr: "commands is required for delegation type",
		},
		{
			name: "valid delegation with commands",
			spec: InstallerSpec{
				Type: InstallTypeDelegation,
				Commands: &CommandsSpec{
					Install: "go install {{.Package}}@{{.Version}}",
				},
			},
			wantErr: "",
		},
		{
			name: "both runtimeRef and toolRef",
			spec: InstallerSpec{
				Type:       InstallTypeDelegation,
				RuntimeRef: "go",
				ToolRef:    "pnpm",
				Commands: &CommandsSpec{
					Install: "some command",
				},
			},
			wantErr: "cannot specify both runtimeRef and toolRef",
		},
		{
			name: "valid with runtimeRef",
			spec: InstallerSpec{
				Type:       InstallTypeDelegation,
				RuntimeRef: "go",
				Commands: &CommandsSpec{
					Install: "go install {{.Package}}@{{.Version}}",
				},
			},
			wantErr: "",
		},
		{
			name: "valid with toolRef",
			spec: InstallerSpec{
				Type:    InstallTypeDelegation,
				ToolRef: "pnpm",
				Commands: &CommandsSpec{
					Install: "pnpm add -g {{.Package}}@{{.Version}}",
				},
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.spec.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.wantErr)
				} else if !containsString(err.Error(), tt.wantErr) {
					t.Errorf("Validate() error = %q, want containing %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestInstallerSpec_Dependencies(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		spec InstallerSpec
		want []Ref
	}{
		{
			name: "no dependencies",
			spec: InstallerSpec{
				Type: InstallTypeDownload,
			},
			want: nil,
		},
		{
			name: "runtimeRef dependency",
			spec: InstallerSpec{
				Type:       InstallTypeDelegation,
				RuntimeRef: "go",
			},
			want: []Ref{{Kind: KindRuntime, Name: "go"}},
		},
		{
			name: "toolRef dependency",
			spec: InstallerSpec{
				Type:    InstallTypeDelegation,
				ToolRef: "pnpm",
			},
			want: []Ref{{Kind: KindTool, Name: "pnpm"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.spec.Dependencies()
			if len(got) != len(tt.want) {
				t.Errorf("Dependencies() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("Dependencies()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
