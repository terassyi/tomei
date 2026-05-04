package resource

import (
	"strings"
	"testing"
)

func TestSystemPackageSpec_Validate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		spec    SystemPackageSpec
		wantErr string
	}{
		{
			name:    "empty installerRef",
			spec:    SystemPackageSpec{Package: "git"},
			wantErr: "installerRef is required",
		},
		{
			name:    "empty package",
			spec:    SystemPackageSpec{InstallerRef: "apt"},
			wantErr: "package is required",
		},
		{
			name:    "valid without repository",
			spec:    SystemPackageSpec{InstallerRef: "apt", Package: "git"},
			wantErr: "",
		},
		{
			name:    "valid with repository",
			spec:    SystemPackageSpec{InstallerRef: "apt", RepositoryRef: "docker", Package: "docker-ce"},
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
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Validate() error = %q, want containing %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestSystemPackageSpec_Dependencies(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		spec SystemPackageSpec
		want []Ref
	}{
		{
			name: "installer only",
			spec: SystemPackageSpec{InstallerRef: "apt", Package: "git"},
			want: []Ref{{Kind: KindSystemInstaller, Name: "apt"}},
		},
		{
			name: "installer and repository",
			spec: SystemPackageSpec{InstallerRef: "apt", RepositoryRef: "docker", Package: "docker-ce"},
			want: []Ref{
				{Kind: KindSystemInstaller, Name: "apt"},
				{Kind: KindSystemPackageRepository, Name: "docker"},
			},
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
