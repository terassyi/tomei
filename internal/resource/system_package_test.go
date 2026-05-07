package resource

import (
	"slices"
	"strings"
	"testing"
)

func TestSystemPackageSetSpec_Validate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		spec    SystemPackageSetSpec
		wantErr string
	}{
		{
			name:    "empty installerRef",
			spec:    SystemPackageSetSpec{Packages: []string{"git"}},
			wantErr: "installerRef is required",
		},
		{
			name:    "empty packages slice",
			spec:    SystemPackageSetSpec{InstallerRef: "apt"},
			wantErr: "at least one package is required",
		},
		{
			name:    "empty package string",
			spec:    SystemPackageSetSpec{InstallerRef: "apt", Packages: []string{""}},
			wantErr: "packages[0] must not be empty",
		},
		{
			name:    "empty package among valid ones",
			spec:    SystemPackageSetSpec{InstallerRef: "apt", Packages: []string{"git", "", "curl"}},
			wantErr: "packages[1] must not be empty",
		},
		{
			name:    "valid",
			spec:    SystemPackageSetSpec{InstallerRef: "apt", Packages: []string{"git"}},
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

// TestSystemPackageImplementsExpandable is a compile-time assertion that
// *SystemPackage satisfies the Expandable interface.
func TestSystemPackageImplementsExpandable(t *testing.T) {
	var _ Expandable = (*SystemPackage)(nil)
}

func TestSystemPackage_Expand_Basic(t *testing.T) {
	t.Parallel()
	sp := &SystemPackage{
		BaseResource:      BaseResource{Metadata: Metadata{Name: "git"}},
		SystemPackageSpec: &SystemPackageSpec{InstallerRef: "apt", Package: "git"},
	}

	got, err := sp.Expand()
	if err != nil {
		t.Fatalf("Expand() unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Expand() returned %d resources, want 1", len(got))
	}
	set, ok := got[0].(*SystemPackageSet)
	if !ok {
		t.Fatalf("Expand()[0] type = %T, want *SystemPackageSet", got[0])
	}
	if set.APIVersion != GroupVersion {
		t.Errorf("expanded APIVersion = %q, want %q", set.APIVersion, GroupVersion)
	}
	if set.Kind() != KindSystemPackageSet {
		t.Errorf("expanded Kind = %s, want %s", set.Kind(), KindSystemPackageSet)
	}
	if set.SystemPackageSetSpec.InstallerRef != "apt" {
		t.Errorf("expanded InstallerRef = %q, want %q", set.SystemPackageSetSpec.InstallerRef, "apt")
	}
	if set.SystemPackageSetSpec.RepositoryRef != "" {
		t.Errorf("expanded RepositoryRef = %q, want empty", set.SystemPackageSetSpec.RepositoryRef)
	}
}

func TestSystemPackage_Expand_WithRepository(t *testing.T) {
	t.Parallel()
	sp := &SystemPackage{
		BaseResource: BaseResource{Metadata: Metadata{Name: "docker-ce"}},
		SystemPackageSpec: &SystemPackageSpec{
			InstallerRef:  "apt",
			RepositoryRef: "docker-repo",
			Package:       "docker-ce",
		},
	}

	got, err := sp.Expand()
	if err != nil {
		t.Fatalf("Expand() unexpected error: %v", err)
	}
	set := got[0].(*SystemPackageSet)
	if set.SystemPackageSetSpec.RepositoryRef != "docker-repo" {
		t.Errorf("expanded RepositoryRef = %q, want %q", set.SystemPackageSetSpec.RepositoryRef, "docker-repo")
	}
}

// TestSystemPackage_Expand_PackageDiffersFromName is the load-bearing test
// for the Name/Package decoupling decision: Packages[0] must come from
// spec.Package, NOT sp.Name(). Real-world: name="docker", package="docker-ce".
func TestSystemPackage_Expand_PackageDiffersFromName(t *testing.T) {
	t.Parallel()
	sp := &SystemPackage{
		BaseResource: BaseResource{Metadata: Metadata{Name: "docker"}},
		SystemPackageSpec: &SystemPackageSpec{
			InstallerRef: "apt",
			Package:      "docker-ce",
		},
	}

	got, err := sp.Expand()
	if err != nil {
		t.Fatalf("Expand() unexpected error: %v", err)
	}
	set := got[0].(*SystemPackageSet)
	if set.Name() != "docker" {
		t.Errorf("expanded Name = %q, want %q (resource identity)", set.Name(), "docker")
	}
	if got, want := set.SystemPackageSetSpec.Packages, []string{"docker-ce"}; !slices.Equal(got, want) {
		t.Errorf("expanded Packages = %v, want %v (must use spec.Package, not sp.Name())", got, want)
	}
}

func TestSystemPackage_Expand_NilSpec(t *testing.T) {
	t.Parallel()
	sp := &SystemPackage{
		BaseResource: BaseResource{Metadata: Metadata{Name: "git"}},
	}

	_, err := sp.Expand()
	if err == nil {
		t.Fatal("Expand() with nil spec: want error, got nil")
	}
	if !strings.Contains(err.Error(), "nil spec") {
		t.Errorf("Expand() error = %q, want containing %q", err.Error(), "nil spec")
	}
}

// TestSystemPackage_Expand_Verbatim is a regression fence for the
// "no normalization, no validation" contract on Package strings.
// If a future change adds rejection or rewriting in Expand(), these
// cases break and document what real-world (and hostile) inputs are
// silently passed through to the installer adapter.
func TestSystemPackage_Expand_Verbatim(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"apt version pin": "nodejs=18.*",
		"apt multiarch":   "libc6:i386",
		"newline":         "git\nrm -rf /", // hostile: must NOT be normalized here
		"leading dash":    "--reinstall",   // hostile: flag-injection style; argv hardening lives in #190
		"nul byte":        "git\x00rm",     // hostile: should round-trip; rejection (if any) belongs at installer boundary
	}
	for name, pkg := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sp := &SystemPackage{
				BaseResource:      BaseResource{Metadata: Metadata{Name: "x"}},
				SystemPackageSpec: &SystemPackageSpec{InstallerRef: "apt", Package: pkg},
			}
			got, err := sp.Expand()
			if err != nil {
				t.Fatalf("Expand(%q) error: %v", pkg, err)
			}
			set := got[0].(*SystemPackageSet)
			if got, want := set.SystemPackageSetSpec.Packages, []string{pkg}; !slices.Equal(got, want) {
				t.Errorf("Packages = %v, want %v (verbatim)", got, want)
			}
		})
	}
}
