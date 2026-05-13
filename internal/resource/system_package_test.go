package resource

import (
	"reflect"
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

// validAptSource returns a minimal-valid AptSource for use as the
// baseline that table-driven tests mutate per case.
func validAptSource() *AptSource {
	return &AptSource{
		URL:        "https://download.docker.com/linux/ubuntu",
		KeyURL:     "https://download.docker.com/linux/ubuntu/gpg",
		KeyHash:    "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		Suite:      "jammy",
		Components: []string{"stable"},
	}
}

func TestAptSource_Validate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(*AptSource)
		wantErr string
	}{
		{name: "valid baseline", mutate: nil},
		{name: "valid with allowed options", mutate: func(a *AptSource) {
			a.Options = map[string]string{"arch": "amd64", "by-hash": "yes"}
		}},
		{name: "empty url", mutate: func(a *AptSource) { a.URL = "" }, wantErr: "apt.url is required"},
		{name: "empty keyUrl", mutate: func(a *AptSource) { a.KeyURL = "" }, wantErr: "apt.keyUrl is required"},
		{name: "empty keyHash", mutate: func(a *AptSource) { a.KeyHash = "" }, wantErr: "apt.keyHash is required"},
		{name: "keyHash wrong algorithm rejected", mutate: func(a *AptSource) { a.KeyHash = "sha512:abc" }, wantErr: "does not match required form"},
		{name: "keyHash uppercase hex rejected", mutate: func(a *AptSource) {
			a.KeyHash = "sha256:ABCDEFABCDEFABCDEFABCDEFABCDEFABCDEFABCDEFABCDEFABCDEFABCDEFABCD"
		}, wantErr: "does not match required form"},
		{name: "keyHash short rejected", mutate: func(a *AptSource) { a.KeyHash = "sha256:00" }, wantErr: "does not match required form"},
		{name: "empty suite", mutate: func(a *AptSource) { a.Suite = "" }, wantErr: "apt.suite is required"},
		{name: "flat repo suite slash rejected", mutate: func(a *AptSource) { a.Suite = "/" }, wantErr: "flat repository"},
		{name: "flat repo suite dotslash rejected", mutate: func(a *AptSource) { a.Suite = "./" }, wantErr: "flat repository"},
		{name: "flat repo suite dot rejected", mutate: func(a *AptSource) { a.Suite = "." }, wantErr: "flat repository"},
		{name: "flat repo suite dotdot rejected", mutate: func(a *AptSource) { a.Suite = ".." }, wantErr: "flat repository"},
		{name: "empty components", mutate: func(a *AptSource) { a.Components = nil }, wantErr: "components must have at least one entry"},
		{name: "empty component string", mutate: func(a *AptSource) { a.Components = []string{""} }, wantErr: "components[0] must not be empty"},
		// signed-by is auto-derived; manifests must not set it.
		{name: "signed-by in options rejected", mutate: func(a *AptSource) {
			a.Options = map[string]string{"signed-by": "/etc/apt/keyrings/foo.gpg"}
		}, wantErr: "auto-derived from metadata.name"},
		// trusted=yes disables signature verification.
		{name: "trusted=yes rejected", mutate: func(a *AptSource) {
			a.Options = map[string]string{"trusted": "yes"}
		}, wantErr: `apt.options["trusted"]`},
		// allow-insecure and allow-weak / allow-downgrade-to-insecure are
		// equivalent to trusted=yes in effect — they relax or disable
		// signature verification.
		{name: "allow-insecure rejected", mutate: func(a *AptSource) {
			a.Options = map[string]string{"allow-insecure": "yes"}
		}, wantErr: `apt.options["allow-insecure"]`},
		{name: "allow-weak rejected", mutate: func(a *AptSource) {
			a.Options = map[string]string{"allow-weak": "yes"}
		}, wantErr: `apt.options["allow-weak"]`},
		{name: "allow-downgrade-to-insecure rejected", mutate: func(a *AptSource) {
			a.Options = map[string]string{"allow-downgrade-to-insecure": "yes"}
		}, wantErr: `apt.options["allow-downgrade-to-insecure"]`},
		{name: "unknown option rejected", mutate: func(a *AptSource) {
			a.Options = map[string]string{"bogus-option": "x"}
		}, wantErr: `apt.options["bogus-option"]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := validAptSource()
			if tt.mutate != nil {
				tt.mutate(a)
			}
			err := a.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() error = %q, want containing %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestSystemPackageRepositorySpec_Validate exercises the discriminator
// dispatch: a non-empty InstallerRef plus the matching per-installer
// source pointer is required. Adding a new arm (dnf, apk, pacman per
// issue #213) means adding a new case here AND in the reconciler — the
// exhaustiveness sanity test below pins that invariant.
func TestSystemPackageRepositorySpec_Validate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		spec    SystemPackageRepositorySpec
		wantErr string
	}{
		{name: "empty installerRef", spec: SystemPackageRepositorySpec{}, wantErr: "installerRef is required"},
		{name: "unknown installerRef", spec: SystemPackageRepositorySpec{InstallerRef: "dnf"}, wantErr: `unsupported installerRef "dnf"`},
		{name: "apt with nil Apt", spec: SystemPackageRepositorySpec{InstallerRef: "apt"}, wantErr: "apt source block is required"},
		{name: "apt with valid Apt", spec: SystemPackageRepositorySpec{InstallerRef: "apt", Apt: validAptSource()}, wantErr: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.spec.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() error = %q, want containing %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestSystemPackageRepositorySpec_ArmsRegistered is a reflection-driven
// exhaustiveness sanity check: every pointer field on
// SystemPackageRepositorySpec (each representing one installer arm of the
// discriminated union) must have a corresponding case in Validate that
// returns a non-empty source for that arm. Today only Apt is wired; when
// dnf / apk / pacman arms land per #213, this test pins the invariant
// "you cannot add a *Source field without also extending Validate to
// recognize it."
//
// The check is "Validate accepts an installerRef matching the field
// name (lowercased)" — adjust the mapping if a future arm's field name
// differs from its installerRef.
func TestSystemPackageRepositorySpec_ArmsRegistered(t *testing.T) {
	t.Parallel()
	specT := reflect.TypeFor[SystemPackageRepositorySpec]()
	for i := 0; i < specT.NumField(); i++ {
		f := specT.Field(i)
		if f.Type.Kind() != reflect.Ptr {
			continue
		}
		installerRef := strings.ToLower(f.Name)
		spec := SystemPackageRepositorySpec{InstallerRef: installerRef}
		// Populate the matching pointer field via reflection so the spec
		// has a non-nil source for its declared installerRef. The source
		// itself is zero-valued, so Validate of the source will fail —
		// but the dispatch case must EXIST. A missing case manifests as
		// `unsupported installerRef "<lower>"`, which the test rejects.
		v := reflect.ValueOf(&spec).Elem()
		fv := v.Field(i)
		fv.Set(reflect.New(f.Type.Elem()))
		err := spec.Validate()
		// We expect a non-nil error (the zero-valued source fails its
		// own field checks) — but the error MUST NOT be "unsupported
		// installerRef", which is the signal that Validate's switch is
		// missing the arm.
		if err != nil && strings.Contains(err.Error(), "unsupported installerRef") {
			t.Errorf("SystemPackageRepositorySpec has *%s field %q but Validate does not dispatch to it; add a case for installerRef=%q in SystemPackageRepositorySpec.Validate",
				f.Type.Elem().Name(), f.Name, installerRef)
		}
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
