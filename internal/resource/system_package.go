package resource

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// keyHashRE pins AptSource.KeyHash to the same `sha256:<64-lowercase-hex>`
// shape the CUE schema enforces. Defense-in-depth for non-CUE callers
// and a clearer fail-fast than the eventual checksum mismatch.
var keyHashRE = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// InstallerRefApt is the canonical string value of
// SystemPackageRepositorySpec.InstallerRef for the only implemented
// arm of the discriminated union today. Exported so reconciler /
// installer / test code that switches on installerRef can reference
// the single source of truth instead of stringly-typed literals.
const InstallerRefApt = "apt"

// aptOptionSignedBy is the bracket-option key whose use in
// AptSource.Options is explicitly forbidden (the keyring path is
// auto-derived from metadata.name).
const aptOptionSignedBy = "signed-by"

// aptOptionArch is the bracket-option key declaring per-architecture
// restrictions in a sources.list entry (e.g. `arch=amd64`). The most
// commonly set option; exported indirectly through AllowedAptOptionKeys.
const aptOptionArch = "arch"

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
	case InstallerRefApt:
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
// bracketed sources.list options restricted to the allowlist exposed
// by IsAllowedAptOption / AllowedAptOptionKeys —
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

// allowedAptOptions is the whitelist of bracket-option keys permitted
// in AptSource.Options. APT understands many more, but the keys below
// cover all realistic third-party-repository needs while excluding
// security-regression knobs (trusted=yes, allow-insecure, allow-weak,
// allow-downgrade-to-insecure — all of which weaken or disable signature
// verification). signed-by is also excluded: the keyring path is
// auto-derived from metadata.name (/usr/share/keyrings/<name>.gpg) and
// must not be overridden via Options. The same allowlist is mirrored
// into the CUE schema's #AptSource.options constraint so tomei validate
// rejects disallowed keys before tomei apply runs.
//
// Kept unexported so callers cannot mutate the underlying set at
// runtime and change validation behavior. Use IsAllowedAptOption /
// AllowedAptOptionKeys for read access (the latter returns a fresh
// sorted copy so callers can't smuggle a mutation through the slice).
var allowedAptOptions = map[string]struct{}{
	aptOptionArch:       {},
	"target":            {},
	"by-hash":           {},
	"pdiffs":            {},
	"check-valid-until": {},
	"lang":              {},
}

// IsAllowedAptOption reports whether the given bracket-option key is
// permitted in AptSource.Options. Callers outside this package should
// use this helper rather than reaching into the map directly.
func IsAllowedAptOption(key string) bool {
	_, ok := allowedAptOptions[key]
	return ok
}

// AllowedAptOptionKeys returns a fresh sorted slice of the allowed
// bracket-option keys. The slice is a copy — mutating it does not
// affect future validations.
func AllowedAptOptionKeys() []string {
	keys := make([]string, 0, len(allowedAptOptions))
	for k := range allowedAptOptions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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
	// the literal `./` (relative-path marker), not `/`. Reject any
	// suite whose first character is `.` or `/`: that includes all
	// flat-style markers (`/`, `./`, `.`, `..`) plus partial-path
	// variants like `.foo` or `/foo` that CUE rejects via its
	// `^[^./]` constraint. The set of legitimate APT suites
	// (codenames like `jammy`, version numbers like `21.04`) never
	// starts with either character, so this is a hard match against
	// the CUE rule and closes the gap for non-CUE callers.
	if r := a.Suite[0]; r == '.' || r == '/' {
		return fmt.Errorf("apt.suite=%q must not start with %q (flat-repository markers and partial paths are not supported)", a.Suite, string(r))
	}
	// URL and KeyURL must be HTTPS (or http://localhost for tests, in
	// line with download.validateDownloadURL — duplicated here because
	// the resource layer is a leaf that download.Downloader imports).
	// The CUE schema enforces #HTTPSURL but non-CUE callers slip through
	// without this gate, contradicting the docstring's "HTTPS only"
	// security claim.
	if err := validateRepoURL("apt.url", a.URL); err != nil {
		return err
	}
	if err := validateRepoURL("apt.keyUrl", a.KeyURL); err != nil {
		return err
	}
	// URL and Suite tokens flow verbatim into the rendered sources.list
	// line. Reject whitespace / line-ending / NUL / control chars at
	// validate time so `tomei validate` mirrors the same rules
	// buildSourcesListLine enforces at apply time; without this, a
	// manifest like `apt.suite: "jammy main"` (extra token) passes
	// validate but breaks apply mid-Install.
	if err := validateSourcesListToken("apt.url", a.URL); err != nil {
		return err
	}
	if err := validateSourcesListToken("apt.suite", a.Suite); err != nil {
		return err
	}
	if len(a.Components) == 0 {
		return fmt.Errorf("apt.components must have at least one entry")
	}
	for i, c := range a.Components {
		if c == "" {
			return fmt.Errorf("apt.components[%d] must not be empty", i)
		}
		if err := validateSourcesListToken(fmt.Sprintf("apt.components[%d]", i), c); err != nil {
			return err
		}
	}
	for k, v := range a.Options {
		if k == aptOptionSignedBy {
			return fmt.Errorf(`apt.options["signed-by"] is auto-derived from metadata.name; remove it from spec.apt.options`)
		}
		if !IsAllowedAptOption(k) {
			return fmt.Errorf("apt.options[%q] is not allowed", k)
		}
		if v == "" {
			return fmt.Errorf("apt.options[%q] must not be empty", k)
		}
		if err := validateOptionValueChars(k, v); err != nil {
			return err
		}
	}
	return nil
}

// validateRepoURL enforces HTTPS-only on AptSource URL fields, with a
// narrow http://localhost (or 127.0.0.1) escape hatch matching
// download.validateDownloadURL so integration tests using
// httptest.NewServer keep working. Duplicated rather than imported
// because download imports this package; the test pinning the two
// stay-in-sync would live in a follow-up if drift becomes a concern.
func validateRepoURL(field, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%s %q is not a valid URL: %w", field, rawURL, err)
	}
	// Order matters here: a bare host like `example.com/repo` parses as
	// Scheme="" + Host="" (everything becomes a path), and a literal
	// hostname like `example.com` similarly has no scheme. Report the
	// missing-scheme case first so the operator gets the actionable
	// "use https://" message instead of the less-specific
	// "not a hierarchical https URL" branch below.
	if u.Scheme == "" {
		return fmt.Errorf("%s %q has no scheme; use https://", field, rawURL)
	}
	// Require a hierarchical URL (scheme://authority/path) — url.Parse
	// happily accepts opaque forms like "https:example.com" where the
	// scheme is set but `//` and host are missing, which CUE's
	// `^https://` regex would reject. The combination Opaque=="" +
	// Host!="" rules these out.
	if u.Opaque != "" || u.Host == "" {
		return fmt.Errorf("%s %q is not a hierarchical https URL (missing `//` or host)", field, rawURL)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if h := u.Hostname(); h == "localhost" || h == "127.0.0.1" {
			return nil
		}
		return fmt.Errorf("%s %q must use https:// (http:// allowed only for localhost / 127.0.0.1 in tests)", field, rawURL)
	default:
		return fmt.Errorf("%s %q uses unsupported scheme %q; only https:// (and http://localhost for tests) is permitted", field, rawURL, u.Scheme)
	}
}

// validateSourcesListToken rejects characters that break the
// space-separated `URL Suite Components...` portion of a sources.list
// line (whitespace splits the token; CR / LF inject new lines; NUL and
// control bytes corrupt apt's parser). Mirrors the same constraints
// buildSourcesListLine applies at apply time so `tomei validate` and
// `tomei apply` agree.
func validateSourcesListToken(field, value string) error {
	for _, r := range value {
		switch {
		case r == ' ' || r == '\t':
			return fmt.Errorf("%s %q contains whitespace", field, value)
		case r == '\n' || r == '\r' || r == 0:
			return fmt.Errorf("%s %q contains line-ending or NUL byte", field, value)
		case r < 0x20:
			return fmt.Errorf("%s %q contains control character", field, value)
		}
	}
	return nil
}

// validateOptionValueChars rejects characters that break the bracket-
// option syntax `[key=value key=value]` in sources.list (whitespace
// splits options; CR / LF / NUL inject lines; `]`, `[`, and `=` collide
// with the surrounding syntax; control bytes corrupt the parser).
// Mirrors buildSourcesListLine's value-char check so a manifest with
// `apt.options.arch: "amd64\n"` is rejected at validate time, not
// silently passed through to apply.
func validateOptionValueChars(key, value string) error {
	for _, r := range value {
		switch {
		case r == ' ' || r == '\t':
			return fmt.Errorf("apt.options[%q] value contains whitespace", key)
		case r == '\n' || r == '\r' || r == 0:
			return fmt.Errorf("apt.options[%q] value contains line-ending or NUL byte", key)
		case r == ']' || r == '[' || r == '=':
			return fmt.Errorf("apt.options[%q] value contains bracket or equals character", key)
		case r < 0x20:
			return fmt.Errorf("apt.options[%q] value contains control character", key)
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
// installer placed on disk; for APT that is [<keyring path>,
// <sources.list path>], used by Remove as a membership set rather than
// as an ordering hint — Remove validates each recorded path against the
// deterministic per-name destinations and then deletes in the canonical
// sequence (sources.list first, then keyring) regardless of slice
// order. The canonical order avoids a brief window where APT would
// consult a sources.list pointing at an already-removed keyring; the
// fixed ordering also means a tampered state file cannot induce a
// reordered Remove that breaks concurrent apt activity.
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

// ValidateSystemPackageSetStateOverlap is the companion to
// ValidateSystemPackageSetOverlap for the legacy-drift case: even when
// the *desired* resource list has no overlap, state.json may already
// contain overlapping SystemPackageSet entries written by an older
// tomei version (before the desired-state validation existed) or by
// manual edits. Removing or shrinking one of those legacy-overlapping
// sets would then run apt-get remove on a package the other set's
// state still claims to own, with no manifest-side gate to catch it.
//
// Detect the drift early — at the same boundary createSystemEngine
// loads the SystemState store — and refuse to build the engine. The
// user must resolve the drift manually (e.g., by editing state.json
// so only one set claims each package) before subsequent applies.
//
// The signature takes the raw map rather than *state.SystemState to
// keep the resource package free of an inverse dependency on the
// state package (state already imports resource).
func ValidateSystemPackageSetStateOverlap(states map[string]*SystemPackageSetState) error {
	// owners is pkg → set-of-distinct-owner-names. Using a set rather
	// than a slice deduplicates the case where a single state entry's
	// Packages list contains the same package name more than once
	// (legitimate "noisy" state but only one owner) — without this,
	// appending the owner name twice would surface a false overlap.
	owners := make(map[string]map[string]struct{})
	for name, st := range states {
		if st == nil {
			continue
		}
		for _, pkg := range st.Packages {
			if owners[pkg] == nil {
				owners[pkg] = make(map[string]struct{})
			}
			owners[pkg][name] = struct{}{}
		}
	}
	var dupes []string
	for pkg, ownerSet := range owners {
		if len(ownerSet) <= 1 {
			continue
		}
		names := make([]string, 0, len(ownerSet))
		for n := range ownerSet {
			names = append(names, n)
		}
		sort.Strings(names)
		dupes = append(dupes, fmt.Sprintf("package %q recorded as installed by SystemPackageSet %v", pkg, names))
	}
	if len(dupes) == 0 {
		return nil
	}
	sort.Strings(dupes)
	return fmt.Errorf("system package state overlap (resolve state.json manually before re-applying): %s", strings.Join(dupes, "; "))
}

// ValidateSystemPackageSetOverlap rejects manifests where the same OS
// package name is declared by more than one SystemPackageSet. Without
// this check, dropping or shrinking one overlapping set would trigger
// PackageSetInstaller.Remove (or the Install-time upgrade drain) on a
// package that another set still relies on; the other set's state would
// continue to record the package as installed while it had been
// uninstalled from the host, and subsequent applies would observe no
// drift to repair.
//
// Operates on the post-ExpandSets resource list (SystemPackage → 1-
// element SystemPackageSet, so it catches overlap regardless of which
// sugar a manifest used). Callers should invoke this from the same
// place they invoke ExpandSets (validate / plan / apply).
//
// Two SystemPackageSet resources with disjoint Packages are fine. A
// future refcount-aware Remove/Drain could relax this constraint, but
// until that lands the only safe model is "one package, one owner."
//
// Known limitation — cross-set removal cascade: this check rejects
// only DIRECT name overlap. apt's reverse-dependency handling can
// still cascade a Remove on one set's package into a package owned by
// another set (e.g., set A owns `perl`, set B owns `cowsay`; removing
// set A cascades cowsay out from under set B because cowsay depends on
// perl). The within-set variant of this hazard is caught at runtime
// via apt-get -s simulation in PackageSetInstaller.Install's upgrade
// drain. Cross-set cascade protection requires plumbing all-set state
// into Install/Remove (so simulation can compare cascades against the
// union of every other set's packages), which is tracked as a separate
// follow-up. Until that lands, manifest authors splitting a dependency
// chain across multiple SystemPackageSets carry the risk.
//
// See also ValidateSystemPackageSetStateOverlap for the companion
// check that catches legacy state drift (state.json contains
// overlapping entries from an older tomei version where the desired-
// state validation did not yet exist).
func ValidateSystemPackageSetOverlap(resources []Resource) error {
	// owners is pkg → set-of-distinct-owner-names. Set semantics avoid
	// a false overlap when a single SystemPackageSet's Packages list
	// repeats the same package name (e.g., a copy-paste mistake in the
	// manifest — apt-get install handles duplicates without complaint,
	// so this is not its own validation error; we just don't want it
	// to look like an overlap across two sets).
	owners := make(map[string]map[string]struct{})
	for _, r := range resources {
		ps, ok := r.(*SystemPackageSet)
		if !ok {
			continue
		}
		if ps.SystemPackageSetSpec == nil {
			continue
		}
		for _, pkg := range ps.SystemPackageSetSpec.Packages {
			if owners[pkg] == nil {
				owners[pkg] = make(map[string]struct{})
			}
			owners[pkg][r.Name()] = struct{}{}
		}
	}
	var dupes []string
	for pkg, ownerSet := range owners {
		if len(ownerSet) <= 1 {
			continue
		}
		names := make([]string, 0, len(ownerSet))
		for n := range ownerSet {
			names = append(names, n)
		}
		sort.Strings(names)
		dupes = append(dupes, fmt.Sprintf("package %q declared by SystemPackageSet %v", pkg, names))
	}
	if len(dupes) == 0 {
		return nil
	}
	sort.Strings(dupes)
	return fmt.Errorf("system package overlap: %s", strings.Join(dupes, "; "))
}

// GetPackages returns the package list recorded by Install. The executor
// type-asserts this method on the state to surface the prior packages
// through context.WithOldPackages on upgrade/reinstall actions — the apt
// installer uses that signal to uninstall packages dropped from the new
// spec, which the generic "upgrade = install" executor flow would
// otherwise leave orphaned on the host.
func (s *SystemPackageSetState) GetPackages() []string {
	if s == nil {
		return nil
	}
	return s.Packages
}

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
