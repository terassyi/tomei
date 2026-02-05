package resource

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToolSpec_Validate(t *testing.T) {
	tests := []struct {
		name    string
		spec    *ToolSpec
		wantErr string
	}{
		{
			name: "valid download pattern with source",
			spec: &ToolSpec{
				InstallerRef: "aqua",
				Version:      "1.0.0",
				Source: &DownloadSource{
					URL: "https://example.com/tool.tar.gz",
				},
			},
			wantErr: "",
		},
		{
			name: "valid registry pattern",
			spec: &ToolSpec{
				InstallerRef:    "aqua",
				Version:         "2.86.0",
				RegistryPackage: "cli/cli",
			},
			wantErr: "",
		},
		{
			name: "valid registry pattern without version (latest)",
			spec: &ToolSpec{
				InstallerRef:    "aqua",
				RegistryPackage: "cli/cli",
			},
			wantErr: "",
		},
		{
			name: "valid delegation pattern",
			spec: &ToolSpec{
				RuntimeRef: "go",
				Package:    "golang.org/x/tools/gopls",
			},
			wantErr: "",
		},
		{
			name: "valid delegation pattern with version",
			spec: &ToolSpec{
				RuntimeRef: "go",
				Version:    "v0.16.0",
				Package:    "golang.org/x/tools/gopls",
			},
			wantErr: "",
		},
		{
			name:    "missing installerRef and runtimeRef",
			spec:    &ToolSpec{Version: "1.0.0"},
			wantErr: "either installerRef or runtimeRef is required",
		},
		{
			name: "missing version, package, and registryPackage",
			spec: &ToolSpec{
				InstallerRef: "aqua",
			},
			wantErr: "version, package, or registryPackage is required",
		},
		{
			name: "runtimeRef without package",
			spec: &ToolSpec{
				RuntimeRef: "go",
				Version:    "1.0.0",
			},
			wantErr: "package is required when using runtimeRef",
		},
		{
			name: "source and registryPackage both specified",
			spec: &ToolSpec{
				InstallerRef:    "aqua",
				Version:         "1.0.0",
				RegistryPackage: "cli/cli",
				Source: &DownloadSource{
					URL: "https://example.com/tool.tar.gz",
				},
			},
			wantErr: "cannot specify both source and registryPackage",
		},
		{
			name: "registryPackage with non-aqua installer",
			spec: &ToolSpec{
				InstallerRef:    "brew",
				RegistryPackage: "cli/cli",
			},
			wantErr: "registryPackage requires installerRef: aqua",
		},
		{
			name: "registryPackage with runtimeRef (invalid)",
			spec: &ToolSpec{
				RuntimeRef:      "go",
				RegistryPackage: "cli/cli",
				Package:         "example.com/tool",
			},
			wantErr: "registryPackage requires installerRef: aqua",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.spec.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestToolSpec_Dependencies(t *testing.T) {
	tests := []struct {
		name string
		spec *ToolSpec
		want []Ref
	}{
		{
			name: "installerRef only",
			spec: &ToolSpec{
				InstallerRef: "aqua",
				Version:      "1.0.0",
			},
			want: []Ref{{Kind: KindInstaller, Name: "aqua"}},
		},
		{
			name: "runtimeRef only",
			spec: &ToolSpec{
				RuntimeRef: "go",
				Package:    "example.com/tool",
			},
			want: []Ref{{Kind: KindRuntime, Name: "go"}},
		},
		{
			name: "both installerRef and runtimeRef",
			spec: &ToolSpec{
				InstallerRef: "aqua",
				RuntimeRef:   "go",
				Package:      "example.com/tool",
			},
			want: []Ref{
				{Kind: KindInstaller, Name: "aqua"},
				{Kind: KindRuntime, Name: "go"},
			},
		},
		{
			name: "neither (empty deps)",
			spec: &ToolSpec{
				Version: "1.0.0",
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.spec.Dependencies()
			assert.Equal(t, tt.want, got)
		})
	}
}
