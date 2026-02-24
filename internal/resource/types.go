package resource

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Kind represents the type of resource (K8s style).
type Kind string

const (
	// System privilege resources
	KindSystemInstaller         Kind = "SystemInstaller"
	KindSystemPackageRepository Kind = "SystemPackageRepository"
	KindSystemPackageSet        Kind = "SystemPackageSet"

	// User privilege resources
	KindInstaller           Kind = "Installer"
	KindInstallerRepository Kind = "InstallerRepository"
	KindRuntime             Kind = "Runtime"
	KindTool                Kind = "Tool"
	KindToolSet             Kind = "ToolSet"
)

const (
	// ProjectName is the name of the project.
	ProjectName = "tomei"

	// APIGroup is the API group for tomei resources.
	APIGroup = "tomei.terassyi.net"

	// APIVersion is the API version.
	APIVersion = "v1beta1"

	// GroupVersion is the full group/version string.
	GroupVersion = APIGroup + "/" + APIVersion
)

// Ref represents a reference to another resource.
type Ref struct {
	Kind Kind
	Name string
}

// knownKinds maps lowercase kind strings to their canonical Kind constants.
var knownKinds map[string]Kind

func init() {
	kinds := []Kind{
		KindSystemInstaller, KindSystemPackageRepository, KindSystemPackageSet,
		KindInstaller, KindInstallerRepository, KindRuntime,
		KindTool, KindToolSet,
	}
	knownKinds = make(map[string]Kind, len(kinds))
	for _, k := range kinds {
		knownKinds[strings.ToLower(string(k))] = k
	}
}

// NormalizeKind converts a case-insensitive kind string to its canonical PascalCase Kind.
// Returns the canonical Kind and true if found, or the zero Kind and false if unknown.
func NormalizeKind(s string) (Kind, bool) {
	k, ok := knownKinds[strings.ToLower(s)]
	return k, ok
}

// ParseRef parses a "kind/name" string into a Ref.
// The kind part is normalized to its canonical PascalCase form.
// Returns an error if the format is invalid or the kind is unknown.
func ParseRef(s string) (Ref, error) {
	kind, name, ok := strings.Cut(s, "/")
	if !ok || kind == "" || name == "" {
		return Ref{}, fmt.Errorf("invalid resource reference %q, expected format: kind/name", s)
	}
	normalized, known := NormalizeKind(kind)
	if !known {
		return Ref{}, fmt.Errorf("unknown resource kind %q", kind)
	}
	return Ref{Kind: normalized, Name: name}, nil
}

// ParseRefArgs parses 1 or 2 command-line arguments into a Ref.
// Accepted formats:
//   - 1 arg with slash: "kind/name" (delegates to ParseRef)
//   - 2 args: kind name (e.g., "tool" "ripgrep")
//
// Returns an error if zero or more than 2 args are given, or if the kind is unknown.
func ParseRefArgs(args []string) (Ref, error) {
	switch len(args) {
	case 1:
		return ParseRef(args[0])
	case 2:
		kind, ok := NormalizeKind(args[0])
		if !ok {
			return Ref{}, fmt.Errorf("unknown resource kind %q", args[0])
		}
		if args[1] == "" {
			return Ref{}, fmt.Errorf("resource name must not be empty")
		}
		return Ref{Kind: kind, Name: args[1]}, nil
	default:
		return Ref{}, fmt.Errorf("expected 1 or 2 arguments (kind/name or kind name), got %d", len(args))
	}
}

// Spec is the interface that all spec types must implement.
type Spec interface {
	Validate() error
	Dependencies() []Ref
}

// State is the interface that all state types must implement.
// Currently a marker interface for type constraints.
type State interface {
	isState()
}

// StateType is a union type constraint enumerating all concrete State types.
// Use this to constrain generic functions that operate on state values.
type StateType interface {
	ToolState | RuntimeState | InstallerState | InstallerRepositoryState |
		SystemInstallerState | SystemPackageRepositoryState | SystemPackageSetState
}

// Resource is the interface that all tomei resources must implement.
type Resource interface {
	Kind() Kind
	Name() string
	Labels() map[string]string
	Spec() Spec
}

// Expandable is a resource that can be expanded into multiple individual resources.
// Set-type resources (ToolSet, SystemPackageSet, etc.) implement this interface.
type Expandable interface {
	Resource
	// Expand returns the individual resources expanded from this set resource.
	// The set resource itself should not be included in the result.
	Expand() ([]Resource, error)
}

// Metadata holds resource identification information.
type Metadata struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// InstallType represents the installation method for resources (Runtime, Installer).
type InstallType string

const (
	// InstallTypeDownload indicates direct binary download.
	// tomei handles downloading, extracting, and placing binaries.
	// Example: Go runtime from go.dev, aqua installer for GitHub releases.
	InstallTypeDownload InstallType = "download"

	// InstallTypeDelegation indicates delegation to external commands.
	// tomei executes configured install/check/remove commands.
	// Example: Rust via rustup, brew install, go install.
	InstallTypeDelegation InstallType = "delegation"
)

// IsDownload returns true if the type is download.
func (t InstallType) IsDownload() bool {
	return t == InstallTypeDownload
}

// IsDelegation returns true if the type is delegation.
func (t InstallType) IsDelegation() bool {
	return t == InstallTypeDelegation
}

// VersionKind classifies how a version was specified in the manifest.
// This enables correct reconciliation: the comparator can determine
// whether a spec change requires reinstallation based on the kind of
// version that was originally specified.
type VersionKind string

const (
	// VersionExact indicates a specific version string (e.g., "14.1.1", "2.86.0").
	// Reconciler compares spec.Version against state.Version directly.
	VersionExact VersionKind = "exact"

	// VersionLatest indicates the version was unspecified (empty string),
	// meaning "use the latest available". Updates are driven by --sync.
	// Reconciler treats this as unchanged unless the spec switches to
	// a non-empty (exact or alias) version.
	VersionLatest VersionKind = "latest"

	// VersionAlias indicates a named alias (e.g., "stable", "lts").
	// The actual version is resolved at install time and stored in state.Version,
	// while the alias is stored in state.SpecVersion.
	// Reconciler compares spec.Version against state.SpecVersion.
	VersionAlias VersionKind = "alias"
)

// IsLatestVersion returns true if the version string means "use latest".
// Both empty string and "latest" are treated as latest.
func IsLatestVersion(version string) bool {
	return version == "" || version == "latest"
}

// ClassifyVersion determines the VersionKind for a given spec version string.
// Empty string or "latest" → VersionLatest, otherwise VersionExact.
// VersionAlias is only assigned by runtime installers that use ResolveVersion.
func ClassifyVersion(specVersion string) VersionKind {
	if IsLatestVersion(specVersion) {
		return VersionLatest
	}
	return VersionExact
}

// IsExactVersion returns true if the version string represents an exact version number
// (e.g., "1.26.0", "v2.1.4") rather than an alias (e.g., "latest", "stable") or empty string.
// It checks whether the version starts with a digit (after stripping an optional "v" prefix).
func IsExactVersion(version string) bool {
	if version == "" {
		return false
	}
	v := strings.TrimPrefix(version, "v")
	return len(v) > 0 && v[0] >= '0' && v[0] <= '9'
}

// TaintReason represents the reason a resource needs reinstallation.
type TaintReason string

const (
	// TaintReasonRuntimeUpgraded indicates the tool's runtime was upgraded.
	TaintReasonRuntimeUpgraded TaintReason = "runtime_upgraded"

	// TaintReasonSyncUpdate indicates the tool was tainted by --sync for latest-version update.
	TaintReasonSyncUpdate TaintReason = "sync_update"

	// TaintReasonUpdateRequested indicates the user requested an update via --update-tools/--update-runtimes.
	TaintReasonUpdateRequested TaintReason = "update_requested"
)

// CommandSet defines a set of shell commands for install/check/remove operations.
// This is the common type used by BootstrapSpec, CommandsSpec, and RuntimeBootstrapSpec.
// Each command is a string slice; multiple entries are joined with " && " at execution time.
// Commands may support Go template variables depending on the context.
type CommandSet struct {
	// Install is the shell command(s) to install/setup.
	Install []string `json:"install"`

	// Check is the shell command(s) to verify installation.
	// Should exit 0 if installed, non-zero otherwise.
	Check []string `json:"check,omitempty"`

	// Remove is the shell command(s) to uninstall/cleanup.
	Remove []string `json:"remove,omitempty"`
}

// stringField describes a single []string field to be unmarshaled with
// unmarshalStringOrSlice.
type stringField struct {
	name   string
	raw    json.RawMessage
	target *[]string
}

// unmarshalStringOrSlice decodes a JSON value that may be either a string
// or an array of strings. CUE's MarshalJSON serializes single-element
// [...string] lists as bare strings.
func unmarshalStringOrSlice(data json.RawMessage) ([]string, error) {
	if len(data) == 0 || string(data) == "null" {
		return nil, nil
	}
	var slice []string
	if err := json.Unmarshal(data, &slice); err == nil {
		return slice, nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return []string{s}, nil
}

// unmarshalStringFields applies unmarshalStringOrSlice to each field.
// Callers extract []string fields as json.RawMessage via the Alias pattern,
// then pass them here to handle CUE's single-element list serialization.
func unmarshalStringFields(fields []stringField) error {
	for _, f := range fields {
		if len(f.raw) == 0 {
			continue
		}
		val, err := unmarshalStringOrSlice(f.raw)
		if err != nil {
			return fmt.Errorf("%s: %w", f.name, err)
		}
		*f.target = val
	}
	return nil
}

// UnmarshalJSON handles CUE's MarshalJSON quirk where single-element lists
// are serialized as bare strings (e.g., ["cmd"] becomes "cmd").
func (c *CommandSet) UnmarshalJSON(data []byte) error {
	var r struct {
		Install json.RawMessage `json:"install"`
		Check   json.RawMessage `json:"check,omitempty"`
		Remove  json.RawMessage `json:"remove,omitempty"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	return unmarshalStringFields([]stringField{
		{"install", r.Install, &c.Install},
		{"check", r.Check, &c.Check},
		{"remove", r.Remove, &c.Remove},
	})
}

// ToolCommandSet extends CommandSet with update and version resolution for self-managed tools.
// Structurally identical to RuntimeBootstrapSpec — kept separate for semantic clarity
// (runtime bootstrap vs tool self-management) and future divergence.
type ToolCommandSet struct {
	CommandSet

	// Update is an optional command to update the tool in-place.
	// When set and the action is upgrade/reinstall, this command is used instead of Install.
	// Falls back to Install if not specified.
	Update []string `json:"update,omitempty"`

	// ResolveVersion is an optional command to capture the installed version after install/update.
	// Supports "github-release:owner/repo:tagPrefix", "http-text:URL:regex", and shell commands.
	// The resolved version is stored in state.json for display via "tomei get".
	ResolveVersion []string `json:"resolveVersion,omitempty"`
}

// UnmarshalJSON handles CUE's MarshalJSON quirk where single-element lists
// are serialized as bare strings. Delegates Install/Check/Remove to CommandSet
// and handles Update and ResolveVersion separately.
func (t *ToolCommandSet) UnmarshalJSON(data []byte) error {
	// Decode the embedded CommandSet fields (Install, Check, Remove).
	if err := t.CommandSet.UnmarshalJSON(data); err != nil {
		return err
	}
	// Decode the additional Update and ResolveVersion fields.
	var extra struct {
		Update         json.RawMessage `json:"update,omitempty"`
		ResolveVersion json.RawMessage `json:"resolveVersion,omitempty"`
	}
	if err := json.Unmarshal(data, &extra); err != nil {
		return err
	}
	return unmarshalStringFields([]stringField{
		{"update", extra.Update, &t.Update},
		{"resolveVersion", extra.ResolveVersion, &t.ResolveVersion},
	})
}

// BaseResource provides common fields for all resources.
// Embed this in concrete resource types.
type BaseResource struct {
	APIVersion   string   `json:"apiVersion"`
	ResourceKind Kind     `json:"kind"`
	Metadata     Metadata `json:"metadata"`
}

// Name returns the resource name.
func (r *BaseResource) Name() string {
	return r.Metadata.Name
}

// Labels returns the resource labels.
func (r *BaseResource) Labels() map[string]string {
	return r.Metadata.Labels
}
