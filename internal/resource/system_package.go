package resource

import (
	"fmt"
	"time"
)

// SystemPackageRepositorySpec defines a third-party repository.
type SystemPackageRepositorySpec struct {
	InstallerRef string       `json:"installerRef"`
	Source       SourceConfig `json:"source"`
}

// Validate validates the SystemPackageRepositorySpec.
func (s *SystemPackageRepositorySpec) Validate() error {
	if s.InstallerRef == "" {
		return fmt.Errorf("installerRef is required")
	}
	if s.Source.URL == "" {
		return fmt.Errorf("source.url is required")
	}
	if s.Source.KeyURL == "" {
		return fmt.Errorf("source.keyUrl is required")
	}
	if s.Source.KeyHash == "" {
		return fmt.Errorf("source.keyHash is required")
	}
	if s.Source.Suite == "" {
		return fmt.Errorf("source.suite is required")
	}
	if len(s.Source.Components) == 0 {
		return fmt.Errorf("source.components must have at least one entry")
	}
	for i, c := range s.Source.Components {
		if c == "" {
			return fmt.Errorf("source.components[%d] must not be empty", i)
		}
	}
	return nil
}

// Dependencies returns the resources this repository depends on.
func (s *SystemPackageRepositorySpec) Dependencies() []Ref {
	return []Ref{
		{Kind: KindSystemInstaller, Name: s.InstallerRef},
	}
}

// SystemPackageRepository is a concrete resource type for system package repositories.
type SystemPackageRepository struct {
	BaseResource
	SystemPackageRepositorySpec *SystemPackageRepositorySpec `json:"spec"`
}

// Kind returns the resource kind (can be called on nil).
func (*SystemPackageRepository) Kind() Kind { return KindSystemPackageRepository }

// Spec returns the spec as Spec interface.
func (s *SystemPackageRepository) Spec() Spec { return s.SystemPackageRepositorySpec }

// SourceConfig holds repository source configuration.
//
// The fields map to the canonical APT one-line sources.list format:
//
//	deb [<options>] <URL> <Suite> <Components...>
//
// URL is the base mirror (e.g. "https://download.docker.com/linux/ubuntu");
// Suite identifies the distribution release (e.g. "jammy"); Components is
// the non-empty list of pool components ("stable", "main", etc.). KeyURL
// supplies the armored GPG public key to import into a per-repository
// keyring; KeyHash pins its SHA256 (format "sha256:<64-hex>") and is
// required as defense-in-depth against transport / CDN compromise. Options
// carries the bracketed sources.list options (signed-by, arch, etc.).
type SourceConfig struct {
	URL        string            `json:"url"`
	KeyURL     string            `json:"keyUrl"`
	KeyHash    string            `json:"keyHash"`
	Suite      string            `json:"suite"`
	Components []string          `json:"components"`
	Options    map[string]string `json:"options,omitempty"`
}

// SystemPackageSetSpec defines a set of system packages.
type SystemPackageSetSpec struct {
	InstallerRef  string   `json:"installerRef"`
	RepositoryRef string   `json:"repositoryRef,omitempty"`
	Packages      []string `json:"packages"`
}

// Validate validates the SystemPackageSetSpec.
func (s *SystemPackageSetSpec) Validate() error {
	if s.InstallerRef == "" {
		return fmt.Errorf("installerRef is required")
	}
	if len(s.Packages) == 0 {
		return fmt.Errorf("at least one package is required")
	}
	for i, p := range s.Packages {
		if p == "" {
			return fmt.Errorf("packages[%d] must not be empty", i)
		}
	}
	return nil
}

// Dependencies returns the resources this package set depends on.
func (s *SystemPackageSetSpec) Dependencies() []Ref {
	deps := []Ref{
		{Kind: KindSystemInstaller, Name: s.InstallerRef},
	}
	if s.RepositoryRef != "" {
		deps = append(deps, Ref{Kind: KindSystemPackageRepository, Name: s.RepositoryRef})
	}
	return deps
}

// SystemPackageSet is a concrete resource type for system package sets.
type SystemPackageSet struct {
	BaseResource
	SystemPackageSetSpec *SystemPackageSetSpec `json:"spec"`
}

// Kind returns the resource kind (can be called on nil).
func (*SystemPackageSet) Kind() Kind { return KindSystemPackageSet }

// Spec returns the spec as Spec interface.
func (s *SystemPackageSet) Spec() Spec { return s.SystemPackageSetSpec }

// SystemPackageRepositoryState represents the state of a repository.
//
// InstalledFiles records the paths the installer placed on disk in
// install order. By convention the APT installer emits
// [<keyring path>, <sources.list path>] so that Remove can iterate in
// reverse (sources.list first, then keyring) — this avoids a brief
// window where APT would consult a sources.list pointing to a missing
// keyring.
type SystemPackageRepositoryState struct {
	InstallerRef   string       `json:"installerRef"`
	Source         SourceConfig `json:"source"`
	InstalledFiles []string     `json:"installedFiles"`
	UpdatedAt      time.Time    `json:"updatedAt"`
}

func (*SystemPackageRepositoryState) isState() {}

// SystemPackageSetState represents the state of installed system packages.
type SystemPackageSetState struct {
	InstallerRef      string            `json:"installerRef"`
	RepositoryRef     string            `json:"repositoryRef,omitempty"`
	Packages          []string          `json:"packages"`
	InstalledVersions map[string]string `json:"installedVersions"`
	UpdatedAt         time.Time         `json:"updatedAt"`
}

func (*SystemPackageSetState) isState() {}

// SystemPackageSpec defines a single system package; sugar for a 1-element SystemPackageSet.
type SystemPackageSpec struct {
	InstallerRef  string `json:"installerRef"`
	RepositoryRef string `json:"repositoryRef,omitempty"`
	Package       string `json:"package"`
}

// Validate validates the SystemPackageSpec.
func (s *SystemPackageSpec) Validate() error {
	if s.InstallerRef == "" {
		return fmt.Errorf("installerRef is required")
	}
	if s.Package == "" {
		return fmt.Errorf("package is required")
	}
	return nil
}

// Dependencies returns the resources this package depends on.
func (s *SystemPackageSpec) Dependencies() []Ref {
	deps := []Ref{
		{Kind: KindSystemInstaller, Name: s.InstallerRef},
	}
	if s.RepositoryRef != "" {
		deps = append(deps, Ref{Kind: KindSystemPackageRepository, Name: s.RepositoryRef})
	}
	return deps
}

// SystemPackage is a sugar resource for a single system package.
type SystemPackage struct {
	BaseResource
	SystemPackageSpec *SystemPackageSpec `json:"spec"`
}

// Kind returns the resource kind (can be called on nil).
func (*SystemPackage) Kind() Kind { return KindSystemPackage }

// Spec returns the spec as Spec interface.
func (s *SystemPackage) Spec() Spec { return s.SystemPackageSpec }

// Expand expands a SystemPackage into a single-element SystemPackageSet.
// Sugar semantics: the engine never sees SystemPackage, only SystemPackageSet.
func (s *SystemPackage) Expand() ([]Resource, error) {
	if s.SystemPackageSpec == nil {
		return nil, fmt.Errorf("SystemPackage %q has nil spec", s.Name())
	}
	return []Resource{
		&SystemPackageSet{
			BaseResource: BaseResource{
				APIVersion:   GroupVersion,
				ResourceKind: KindSystemPackageSet,
				Metadata:     Metadata{Name: s.Name()},
			},
			SystemPackageSetSpec: &SystemPackageSetSpec{
				InstallerRef:  s.SystemPackageSpec.InstallerRef,
				RepositoryRef: s.SystemPackageSpec.RepositoryRef,
				Packages:      []string{s.SystemPackageSpec.Package},
			},
		},
	}, nil
}
