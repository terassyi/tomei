package resource

import (
	"strings"
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
					Install: []string{"go install {{.Package}}@{{.Version}}"},
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
					Install: []string{"some command"},
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
					Install: []string{"go install {{.Package}}@{{.Version}}"},
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
					Install: []string{"pnpm add -g {{.Package}}@{{.Version}}"},
				},
			},
			wantErr: "",
		},
		{
			name: "delegation with dependsOn",
			spec: InstallerSpec{
				Type:      InstallTypeDelegation,
				ToolRef:   "krew",
				DependsOn: []string{"kubectl"},
				Commands: &CommandsSpec{
					Install: []string{"krew install {{.Package}}"},
				},
			},
			wantErr: "",
		},
		{
			name: "download with dependsOn",
			spec: InstallerSpec{
				Type:      InstallTypeDownload,
				DependsOn: []string{"kubectl"},
			},
			wantErr: "",
		},
		{
			name: "dependsOn overlaps toolRef (tolerated)",
			spec: InstallerSpec{
				Type:      InstallTypeDelegation,
				ToolRef:   "krew",
				DependsOn: []string{"krew"},
				Commands: &CommandsSpec{
					Install: []string{"krew install {{.Package}}"},
				},
			},
			wantErr: "",
		},
		{
			name: "dependsOn has duplicates",
			spec: InstallerSpec{
				Type:      InstallTypeDelegation,
				DependsOn: []string{"kubectl", "kubectl"},
				Commands: &CommandsSpec{
					Install: []string{"some command"},
				},
			},
			wantErr: "dependsOn contains duplicate entry",
		},
		{
			name: "dependsOn has empty string",
			spec: InstallerSpec{
				Type:      InstallTypeDownload,
				DependsOn: []string{""},
			},
			wantErr: "dependsOn must not contain empty strings",
		},
		{
			name: "valid delegation with binDir tilde",
			spec: InstallerSpec{
				Type:   InstallTypeDelegation,
				BinDir: "~/.krew/bin",
				Commands: &CommandsSpec{
					Install: []string{"krew install {{.Package}}"},
				},
			},
			wantErr: "",
		},
		{
			name: "valid delegation with binDir absolute",
			spec: InstallerSpec{
				Type:   InstallTypeDelegation,
				BinDir: "/opt/bin",
				Commands: &CommandsSpec{
					Install: []string{"some command"},
				},
			},
			wantErr: "",
		},
		{
			name: "binDir with relative path",
			spec: InstallerSpec{
				Type:   InstallTypeDelegation,
				BinDir: "relative/path",
				Commands: &CommandsSpec{
					Install: []string{"some command"},
				},
			},
			wantErr: "binDir must be an absolute path",
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
				} else if !strings.Contains(err.Error(), tt.wantErr) {
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
		{
			name: "toolRef and dependsOn",
			spec: InstallerSpec{
				Type:      InstallTypeDelegation,
				ToolRef:   "krew",
				DependsOn: []string{"kubectl"},
			},
			want: []Ref{
				{Kind: KindTool, Name: "krew"},
				{Kind: KindTool, Name: "kubectl"},
			},
		},
		{
			name: "dependsOn only",
			spec: InstallerSpec{
				Type:      InstallTypeDownload,
				DependsOn: []string{"kubectl", "helm"},
			},
			want: []Ref{
				{Kind: KindTool, Name: "kubectl"},
				{Kind: KindTool, Name: "helm"},
			},
		},
		{
			name: "runtimeRef and dependsOn",
			spec: InstallerSpec{
				Type:       InstallTypeDelegation,
				RuntimeRef: "go",
				DependsOn:  []string{"gopls"},
			},
			want: []Ref{
				{Kind: KindRuntime, Name: "go"},
				{Kind: KindTool, Name: "gopls"},
			},
		},
		{
			name: "dependsOn overlaps toolRef (deduplicated)",
			spec: InstallerSpec{
				Type:      InstallTypeDelegation,
				ToolRef:   "krew",
				DependsOn: []string{"krew", "kubectl"},
			},
			want: []Ref{
				{Kind: KindTool, Name: "krew"},
				{Kind: KindTool, Name: "kubectl"},
			},
		},
		{
			name: "dependsOn empty list",
			spec: InstallerSpec{
				Type:      InstallTypeDownload,
				DependsOn: []string{},
			},
			want: nil,
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
