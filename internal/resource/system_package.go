package resource

import (
	"fmt"
	"regexp"
	"time"
)

// keyHashRE pins AptSource.KeyHash to the same `sha256:<64-lowercase-hex>`
// shape the CUE schema enforces. Defense-in-depth for non-CUE callers
// and a clearer fail-fast than the eventual checksum mismatch.
var keyHashRE = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// SystemPackageRepositorySpec defines a third-party repository. It is a
// discriminated union keyed by InstallerRef: exactly one of the
// installer-specific source pointers (Apt, ...) is non-nil and must
// match InstallerRef. Adding a new installer means adding a new pointer
// field and an arm in Validate; existing fields are unaffected. The CUE
// schema enforces the same shape statically via #SystemPackageRepository.
type SystemPackageRepositorySpec struct {
	InstallerRef string     `json:"installerRef"`
	Apt          *AptSource `json:"apt,omitempty"`
}

// Validate validates the SystemPackageRepositorySpec.
func (s *SystemPackageRepositorySpec) Validate() error {
	if s.InstallerRef == "" {
		return fmt.Errorf("installerRef is required")
	}
	switch s.InstallerRef {
	case "apt":
		if s.Apt == nil {
			return fmt.Errorf("apt source block is required when installerRef is %q", s.InstallerRef)
		}
		return s.Apt.Validate()
	default:
		return fmt.Errorf("unsupported installerRef %q", s.InstallerRef)
	}
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

// AptSource holds the source configuration for an APT third-party
// repository.
//
// The fields map to the canonical APT one-line sources.list format:
//
//	deb [<options>] <URL> <Suite> <Components...>
//
// All fields (URL, KeyURL, KeyHash, Suite, Components, Options) are
// trust-bound: they originate from a CUE manifest under the user's
// control and flow into the shell-emitted sources.list line through
// library-emitted strings (not template-expanded user input). URL is
// the base mirror (e.g. "https://download.docker.com/linux/ubuntu");
// KeyURL is the HTTPS URL of the armored GPG public key (which may
// legitimately be served from a different host than URL); KeyHash pins
// the armored key's SHA256 (format "sha256:<64-hex>") as defense-in-depth
// against transport / CDN compromise; Suite identifies the distribution
// release (e.g. "jammy" — single-suite only by design; "/" / flat
// repositories are explicitly unsupported); Components is the non-empty
// list of pool components ("stable", "main", etc.); Options carries the
// bracketed sources.list options restricted to AllowedAptOptions —
// signed-by is auto-derived from metadata.name and must not be supplied
// here.
type AptSource struct {
	URL        string            `json:"url"`
	KeyURL     string            `json:"keyUrl"`
	KeyHash    string            `json:"keyHash"`
	Suite      string            `json:"suite"`
	Components []string          `json:"components"`
	Options    map[string]string `json:"options,omitempty"`
}

// AllowedAptOptions is the whitelist of bracket-option keys permitted
// in AptSource.Options. APT understands many more, but the keys below
// cover all realistic third-party-repository needs while excluding
// security-regression knobs (trusted=yes, allow-insecure, allow-weak,
// allow-downgrade-to-insecure — all of which weaken or disable signature
// verification). signed-by is also excluded: the keyring path is
// auto-derived from metadata.name (/usr/share/keyrings/<name>.gpg) and
// must not be overridden via Options. The same allowlist is mirrored
// into the CUE schema's #AptSource.options constraint so tomei validate
// rejects disallowed keys before tomei apply runs.
var AllowedAptOptions = map[string]struct{}{
	"arch":              {},
	"target":            {},
	"by-hash":           {},
	"pdiffs":            {},
	"check-valid-until": {},
	"lang":              {},
}

// Validate validates the AptSource fields. Empty / missing required
// fields return a descriptive error rooted at "apt.<field>" so callers
// know which arm of the discriminated union failed.
func (a *AptSource) Validate() error {
	if a.URL == "" {
		return fmt.Errorf("apt.url is required")
	}
	if a.KeyURL == "" {
		return fmt.Errorf("apt.keyUrl is required")
	}
	if a.KeyHash == "" {
		return fmt.Errorf("apt.keyHash is required")
	}
	if !keyHashRE.MatchString(a.KeyHash) {
		return fmt.Errorf("apt.keyHash %q does not match required form sha256:<64-lowercase-hex>", a.KeyHash)
	}
	if a.Suite == "" {
		return fmt.Errorf("apt.suite is required")
	}
	// APT flat-repository syntax is `deb URL ./` — the suite token is
	// the literal `./` (relative-path marker), not `/`. Reject all flat-
	// style markers explicitly so manifests cannot smuggle a flat layout
	// past the schema by varying the spelling.
	switch a.Suite {
	case "/", "./", ".", "..":
		return fmt.Errorf("apt.suite=%q (flat repository layout) is not supported", a.Suite)
	}
	if len(a.Components) == 0 {
		return fmt.Errorf("apt.components must have at least one entry")
	}
	for i, c := range a.Components {
		if c == "" {
			return fmt.Errorf("apt.components[%d] must not be empty", i)
		}
	}
	for k := range a.Options {
		if k == "signed-by" {
			return fmt.Errorf(`apt.options["signed-by"] is auto-derived from metadata.name; remove it from spec.apt.options`)
		}
		if _, ok := AllowedAptOptions[k]; !ok {
			return fmt.Errorf("apt.options[%q] is not allowed", k)
		}
	}
	return nil
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
// The state mirrors the discriminated-union shape of the spec: exactly
// one of the installer-specific source pointers (Apt, ...) is non-nil
// and matches InstallerRef. InstalledFiles records the paths the
// installer placed on disk in install order. By convention the APT
// installer emits [<keyring path>, <sources.list path>] so that Remove
// can iterate in reverse (sources.list first, then keyring) — this
// avoids a brief window where APT would consult a sources.list pointing
// to a missing keyring.
type SystemPackageRepositoryState struct {
	InstallerRef   string     `json:"installerRef"`
	Apt            *AptSource `json:"apt,omitempty"`
	InstalledFiles []string   `json:"installedFiles"`
	UpdatedAt      time.Time  `json:"updatedAt"`
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
