package resource

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/terassyi/tomei/internal/checksum"
	"github.com/terassyi/tomei/internal/installer/extract"
)

// shaPattern matches a 40-character lowercase hex SHA-1, the form
// `go install` resolves through GOPROXY+GOSUMDB. Short SHAs are rejected to
// avoid proxy-side prefix-resolution ambiguity and downstream collision risk.
var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// DownloadSource holds download configuration for tools and runtimes
// that are installed via the download pattern (e.g., aqua installer).
type DownloadSource struct {
	// URL is the download URL for the tool archive or binary.
	// Must be HTTPS. Supports GitHub releases, direct downloads, etc.
	// Example: "https://github.com/cli/cli/releases/download/v2.62.0/gh_2.62.0_linux_amd64.tar.gz"
	URL string `json:"url"`

	// Checksum configures how to verify the downloaded file's integrity.
	// Either a direct value or a URL to a checksums file can be specified.
	Checksum *Checksum `json:"checksum,omitempty"`

	// ArchiveType specifies the archive format explicitly.
	// If empty, the type is auto-detected from the URL extension.
	// See extract.ArchiveTypeTarGz, extract.ArchiveTypeZip, extract.ArchiveTypeRaw.
	ArchiveType extract.ArchiveType `json:"archiveType,omitempty"`
}

// Package is a universal package identifier that can represent different package formats
// depending on the installer or runtime being used.
//
// For registry-based installation (e.g., aqua):
//
//	package: { owner: "cli", repo: "cli" }
//
// For name-based installation (e.g., go install, cargo install):
//
//	package: { name: "golang.org/x/tools/gopls" }
type Package struct {
	// Owner is the GitHub organization or user name.
	// Used for registry-based installation (e.g., aqua).
	// Example: "cli", "BurntSushi", "sharkdp"
	Owner string `json:"owner,omitempty"`

	// Repo is the GitHub repository name.
	// Used for registry-based installation (e.g., aqua).
	// Example: "cli", "ripgrep", "fd"
	Repo string `json:"repo,omitempty"`

	// Name is the package identifier for delegation-based installation.
	// Format depends on the runtime/installer:
	//   - Go: "golang.org/x/tools/gopls"
	//   - Cargo: "ripgrep"
	//   - npm: "@biomejs/biome"
	Name string `json:"name,omitempty"`
}

// String returns a string representation of the package.
// Returns "owner/repo" format if Owner and Repo are set, otherwise returns Name.
func (p *Package) String() string {
	if p == nil {
		return ""
	}
	if p.Owner != "" && p.Repo != "" {
		return p.Owner + "/" + p.Repo
	}
	return p.Name
}

// IsEmpty returns true if the package is not specified.
func (p *Package) IsEmpty() bool {
	return p == nil || (p.Owner == "" && p.Repo == "" && p.Name == "")
}

// IsRegistry returns true if the package uses registry format (owner/repo).
func (p *Package) IsRegistry() bool {
	return p != nil && p.Owner != "" && p.Repo != ""
}

// IsName returns true if the package uses name format.
func (p *Package) IsName() bool {
	return p != nil && p.Name != ""
}

// Validate checks if the package is valid.
// Either (Owner + Repo) or Name must be specified, but not both.
func (p *Package) Validate() error {
	if p == nil {
		return nil
	}

	hasRegistry := p.Owner != "" || p.Repo != ""
	hasName := p.Name != ""

	// Check mutual exclusivity
	if hasRegistry && hasName {
		return fmt.Errorf("package: cannot specify both owner/repo and name")
	}

	// If registry format, both owner and repo are required
	if p.Owner != "" && p.Repo == "" {
		return fmt.Errorf("package.repo is required when owner is specified")
	}
	if p.Repo != "" && p.Owner == "" {
		return fmt.Errorf("package.owner is required when repo is specified")
	}

	return nil
}

// UnmarshalJSON implements custom JSON unmarshaling for Package.
// It supports both string format and object format:
//   - String: "owner/repo" format is parsed into Owner+Repo (for aqua registry)
//   - String: other formats are stored as Name (for go install, cargo install, etc.)
//   - Object: {"owner": "cli", "repo": "cli"} or {"name": "golang.org/x/tools/gopls"}
func (p *Package) UnmarshalJSON(data []byte) error {
	// Try string format first
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		// Check if it's "owner/repo" format (exactly one slash, no dots before the slash)
		// This distinguishes "BurntSushi/ripgrep" from "golang.org/x/tools/gopls"
		if isRegistryFormat(str) {
			parts := splitOnce(str, '/')
			p.Owner = parts[0]
			p.Repo = parts[1]
		} else {
			// Store as Name for delegation-based installation
			p.Name = str
		}
		return nil
	}

	// Try object format
	type packageAlias Package
	var alias packageAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return fmt.Errorf("package: must be string or object: %w", err)
	}
	*p = Package(alias)
	return nil
}

// isRegistryFormat checks if a string looks like an "owner/repo" or "owner/repo/sub" format
// rather than a package path like "golang.org/x/tools/gopls".
// Registry format: at least one slash, no dots before the first slash, not starting with @.
func isRegistryFormat(s string) bool {
	// npm scoped packages start with @ (e.g., @biomejs/biome)
	if len(s) > 0 && s[0] == '@' {
		return false
	}

	slashIdx := -1
	for i, c := range s {
		if c == '/' {
			if slashIdx == -1 {
				slashIdx = i
			}
		} else if c == '.' && slashIdx == -1 {
			// Dot before first slash - looks like a domain (e.g., golang.org)
			return false
		}
	}
	// Must have at least one slash and non-empty first segment
	return slashIdx > 0 && slashIdx < len(s)-1
}

// splitOnce splits a string on the first occurrence of sep.
func splitOnce(s string, sep byte) [2]string {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return [2]string{s[:i], s[i+1:]}
		}
	}
	return [2]string{s, ""}
}

// Checksum holds checksum verification configuration.
// Either Value or URL should be specified, not both.
type Checksum struct {
	// Value is the direct checksum value in "algorithm:hash" format.
	// Currently only sha256 is supported.
	// Example: "sha256:abc123def456..."
	Value string `json:"value,omitempty"`

	// URL points to a checksums file (e.g., GitHub release SHA256SUMS).
	// The file should contain lines in "hash  filename" format.
	// Example: "https://github.com/cli/cli/releases/download/v2.62.0/checksums.txt"
	URL string `json:"url,omitempty"`

	// FilePattern is a glob pattern to match the target file in the checksums file.
	// Used when URL is specified to identify which line contains our file's hash.
	// If empty, matches against the downloaded filename.
	// Example: "gh_*_linux_amd64.tar.gz"
	FilePattern string `json:"filePattern,omitempty"`

	// Algorithm is the checksum algorithm resolved from the registry (e.g., aqua).
	// When set, it takes priority over auto-detection from hash length.
	// This is not specified in CUE manifests — it is populated internally by resolvers.
	Algorithm checksum.Algorithm `json:"algorithm,omitempty"`
}

// Canonical names of builtin runtimes and installers.
const (
	// RuntimeNameGo is the canonical name of the builtin Go runtime.
	RuntimeNameGo = "go"

	// InstallerNameAqua is the canonical name of the builtin aqua installer.
	// Tools using a Registry package (owner/repo form) must reference this installer.
	InstallerNameAqua = "aqua"
)

// ToolSpec defines the desired state of an individual tool.
// A tool can be installed via four patterns:
//  1. Commands pattern: Self-managed tool with install/update/remove commands
//  2. Download pattern (explicit): Downloads with Source specified
//  3. Download pattern (registry): Uses Package with owner/repo to resolve URL from aqua-registry
//  4. Delegation pattern: Uses Package with name for runtime/installer commands
type ToolSpec struct {
	// InstallerRef references an Installer resource by name.
	// For download pattern: points to an installer like "aqua" that handles downloading.
	// For delegation pattern: points to an installer like "go", "cargo", "npm" that has install commands.
	// Mutually exclusive with RuntimeRef and Commands.
	InstallerRef string `json:"installerRef,omitempty"`

	// RepositoryRef references an InstallerRepository resource by name.
	// Used when the tool is installed from a third-party repository
	// (e.g., a Helm chart from a custom repo, a krew plugin from a custom index).
	// The referenced InstallerRepository must be configured before this tool can be installed.
	RepositoryRef string `json:"repositoryRef,omitempty"`

	// Version specifies the tool version to install.
	// Format depends on the tool (e.g., "2.62.0", "v2.62.0", "latest").
	// Required for download pattern with Source; optional for registry pattern (defaults to latest).
	// Optional for commands pattern (can be resolved via commands.resolveVersion).
	Version string `json:"version,omitempty"`

	// SHA pins installation to a specific git commit SHA instead of a tag/version.
	// Must be a 40-character lowercase hex SHA-1. Mutually exclusive with Version.
	// Currently supported only with RuntimeRef: "go" (go install pkg@<sha> resolves
	// the SHA through GOPROXY+GOSUMDB transparency log). For other installers, SHA
	// is rejected at Validate time — extending support requires re-deriving the
	// integrity model for that installer.
	SHA string `json:"sha,omitempty"`

	// Enabled controls whether this tool should be installed.
	// Default is true. Set to false to skip installation without removing the config.
	Enabled *bool `json:"enabled,omitempty"`

	// Source configures download settings for the download pattern (explicit).
	// Mutually exclusive with Package.
	Source *DownloadSource `json:"source,omitempty"`

	// Package specifies the package identifier.
	// For registry-based installation (aqua): use owner/repo format
	//   package: { owner: "cli", repo: "cli" }
	// For delegation-based installation (go, cargo, npm): use name format
	//   package: { name: "golang.org/x/tools/gopls" }
	// Mutually exclusive with Source.
	Package *Package `json:"package,omitempty"`

	// RuntimeRef references a Runtime resource by name for delegation installation.
	// When set, the tool is installed using the runtime's install command
	// (e.g., "go install" for Go runtime).
	// The tool will be tainted (marked for reinstallation) when the runtime is upgraded.
	// Mutually exclusive with InstallerRef and Commands.
	RuntimeRef string `json:"runtimeRef,omitempty"`

	// Commands configures shell commands for self-managed tool installation.
	// Tools installed via curl scripts or self-contained installers use this field.
	// Mutually exclusive with InstallerRef and RuntimeRef.
	Commands *ToolCommandSet `json:"commands,omitempty"`

	// BinaryName overrides the binary name for placement and symlink.
	// When set, the installed binary and symlink use this name instead of the tool name
	// or aqua registry files[].name. This is useful for tools like krew that need
	// a different binary name (e.g., "kubectl-krew") than the resource name.
	BinaryName string `json:"binaryName,omitempty"`

	// Args provides additional arguments appended to the install command.
	// These are joined with spaces and available as {{.Args}} in command templates.
	// Example: ["--with-executables-from", "ansible-core"] for uv tool install.
	Args []string `json:"args,omitempty"`

	// Privileged is honored only for specific install patterns (Commands and
	// the tomei-managed download/registry patterns); for those it makes the
	// tool require --system (and be skipped without it). For installer-/name-
	// delegation it is ignored, and with RuntimeRef it is a Validate error.
	// Semantics depend on the install pattern:
	//
	//   - Commands: tomei pre-acquires a cached sudo timestamp so "sudo ..."
	//     invocations inside the command run without re-prompting. The
	//     commands themselves execute as the invoking user via "sh -c";
	//     tomei does not wrap them in sudo. If a step must run as root,
	//     write it as "sudo <cmd>" explicitly. Installers that run as the
	//     user and escalate internally (e.g., Homebrew) work with
	//     Privileged alone.
	//
	//   - Download (Source set) / Registry (owner/repo Package via aqua):
	//     tomei downloads and places the binary itself. Today this gates
	//     the --system requirement; the planned follow-up routes these
	//     placements under SystemBinDir (/usr/local/bin) via a sudo-backed
	//     symlink rather than the user bin dir.
	//
	//   - Installer- / name-delegation (InstallerRef with no Source and a
	//     non-registry Package, e.g. "go install" / "cargo install" /
	//     helm): ignored. The installer or runtime owns the destination
	//     directory.
	//
	//   - Runtime delegation (RuntimeRef set): rejected by Validate —
	//     Privileged + RuntimeRef has no coherent meaning.
	//
	// Without --system, privileged tools are skipped.
	//
	// Migration note: earlier versions wrapped the entire command in
	// "sudo -n sh -c ..." when Privileged was set. Manifests that relied
	// on that wrap to run commands as root must now prefix the specific
	// steps that need root with "sudo " (e.g., "cp ..." → "sudo cp ...").
	// Default is false.
	Privileged bool `json:"privileged,omitempty"`
}

// UnmarshalJSON handles CUE's MarshalJSON quirk where single-element lists
// are serialized as bare strings for the Args field.
func (s *ToolSpec) UnmarshalJSON(data []byte) error {
	type Alias ToolSpec
	var r struct {
		Alias
		Args json.RawMessage `json:"args,omitempty"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	*s = ToolSpec(r.Alias)
	return unmarshalStringFields([]stringField{
		{"args", r.Args, &s.Args},
	})
}

// Validate validates the ToolSpec.
func (s *ToolSpec) Validate() error {
	// Exactly one install method: InstallerRef, RuntimeRef, or Commands
	hasInstaller := s.InstallerRef != ""
	hasRuntime := s.RuntimeRef != ""
	hasCommands := s.Commands != nil

	if !hasInstaller && !hasRuntime && !hasCommands {
		return fmt.Errorf("one of installerRef, runtimeRef, or commands is required")
	}
	if (hasInstaller && hasRuntime) || (hasRuntime && hasCommands) || (hasInstaller && hasCommands) {
		return fmt.Errorf("installerRef, runtimeRef, and commands are mutually exclusive")
	}

	// Privileged + RuntimeRef has no coherent semantics: runtime-managed install
	// dirs are owned by the runtime, not tomei, so there is nothing to escalate.
	if s.Privileged && hasRuntime {
		return fmt.Errorf("privileged: true is not supported with runtimeRef")
	}

	if err := s.validateSHA(); err != nil {
		return err
	}

	// Commands must have at least Install
	if hasCommands && len(s.Commands.Install) == 0 {
		return fmt.Errorf("commands.install is required")
	}

	// For non-commands patterns, version/sha/source/package is required.
	// SHA satisfies this gate so a sha-only spec falls through to the more
	// specific runtimeRef/package check below instead of stopping here with
	// a generic "version, source, or package is required" message.
	if !hasCommands {
		if s.Version == "" && s.SHA == "" && s.Source == nil && s.Package.IsEmpty() {
			return fmt.Errorf("version, source, or package is required")
		}
	}

	// Runtime delegation requires package
	if hasRuntime && s.Package.IsEmpty() {
		return fmt.Errorf("package is required when using runtimeRef")
	}

	// Source and Package are mutually exclusive
	if s.Source != nil && !s.Package.IsEmpty() {
		return fmt.Errorf("cannot specify both source and package")
	}

	// Registry package (explicit owner/repo object) requires InstallerRef="aqua"
	if s.Package.IsRegistry() && s.InstallerRef != InstallerNameAqua {
		return fmt.Errorf("package with owner/repo requires installerRef: aqua")
	}

	// Validate Package if specified
	if err := s.Package.Validate(); err != nil {
		return err
	}

	return nil
}

// validateSHA enforces the sha-pin policy: sha pins to a 40-char lowercase
// hex SHA-1 and is currently supported only with `runtimeRef: go`. Other
// installers (aqua, cargo, npm, ...) have no equivalent integrity guarantee,
// so extending support requires re-deriving the supply-chain model — do not
// relax this check casually.
func (s *ToolSpec) validateSHA() error {
	if s.SHA == "" {
		return nil
	}
	if s.Version != "" {
		return fmt.Errorf("sha and version are mutually exclusive")
	}
	if s.RuntimeRef != RuntimeNameGo {
		return fmt.Errorf("sha is only supported with runtimeRef: go (got runtimeRef: %q)", s.RuntimeRef)
	}
	if !shaPattern.MatchString(s.SHA) {
		return fmt.Errorf("sha must be a 40-character lowercase hex SHA-1 (got %q)", s.SHA)
	}
	return nil
}

// Dependencies returns the resources this tool depends on.
func (s *ToolSpec) Dependencies() []Ref {
	var deps []Ref
	if s.InstallerRef != "" {
		deps = append(deps, Ref{Kind: KindInstaller, Name: s.InstallerRef})
	}
	if s.RepositoryRef != "" {
		deps = append(deps, Ref{Kind: KindInstallerRepository, Name: s.RepositoryRef})
	}
	if s.RuntimeRef != "" {
		deps = append(deps, Ref{Kind: KindRuntime, Name: s.RuntimeRef})
	}
	return deps
}

// Tool is a concrete resource type for individual tools.
type Tool struct {
	BaseResource
	ToolSpec *ToolSpec `json:"spec"`
}

// Kind returns the resource kind (can be called on nil).
func (*Tool) Kind() Kind { return KindTool }

// Spec returns the spec as Spec interface.
func (t *Tool) Spec() Spec { return t.ToolSpec }

// IsEnabled returns whether the tool is enabled.
// Implements the Enableable interface.
func (t *Tool) IsEnabled() bool {
	if t.ToolSpec == nil {
		return true
	}
	return t.ToolSpec.IsEnabled()
}

// IsPrivileged reports whether this tool requires --system. The predicate
// honors the privileged flag for two kinds of pattern: Commands (where it
// gates a cached sudo timestamp for the user's commands) and the tomei-managed
// placement patterns, download (Source) and registry (aqua / owner-repo
// Package). For installer- or runtime-delegation the installer/runtime owns
// the destination directory, so privileged is ignored.
//
// Keep in sync with Tool.PrivilegedReason: any new privileged arm added here
// MUST also be wired into that method. PrivilegedReason returns the
// commands reason when Commands != nil and the placement reason otherwise,
// so a new non-commands arm that goes unwired here would silently emit the
// placement reason — misleading rather than obviously broken.
func (t *Tool) IsPrivileged() bool {
	if t.ToolSpec == nil || !t.ToolSpec.Privileged {
		return false
	}
	s := t.ToolSpec
	switch {
	case s.Commands != nil:
		return true
	case s.InstallerRef == "":
		// Without an installer there is no tomei-managed placement to
		// escalate: runtime delegation and any spec lacking an install
		// method fall out here (RuntimeRef additionally fails Validate).
		return false
	case s.Source != nil:
		// Download pattern: tomei downloads and places the binary.
		return true
	case s.Package.IsRegistry():
		// Registry pattern (aqua): tomei resolves the URL and places the
		// binary. Validate enforces that a registry Package requires
		// installerRef: aqua, so we don't re-check the installer here.
		return true
	default:
		// Installer-/name-delegation lets the installer own the
		// destination dir, so privileged is ignored.
		return false
	}
}

// PrivilegedReason returns a short human-readable reason this tool requires
// --system, or "" if the tool is not privileged. Mirrors IsPrivileged's
// pattern arms: Commands → sudo-cached shell commands; download/registry →
// system bin directory placement. Installer-/runtime-delegation never
// returns a non-empty reason since IsPrivileged returns false for them.
//
// The returned string is user-facing; treat as opaque (do not switch on it
// in callers). Keep in sync with Tool.IsPrivileged: any new privileged arm
// added there MUST also be wired into this method.
func (t *Tool) PrivilegedReason() string {
	if !t.IsPrivileged() {
		return ""
	}
	if t.ToolSpec.Commands != nil {
		return "requires sudo cache for shell commands"
	}
	return "places a symlink in the system bin directory requiring sudo"
}

// IsEnabled returns whether the tool spec is enabled.
func (t *ToolSpec) IsEnabled() bool {
	if t.Enabled == nil {
		return true
	}
	return *t.Enabled
}

// ToolSetSpec defines a set of tools that share the same installer configuration.
// This is a convenience for managing multiple tools from the same source
// (e.g., multiple CLI tools from GitHub releases via aqua, or Go tools via go install).
type ToolSetSpec struct {
	// InstallerRef references the shared Installer resource for all tools in this set.
	// All tools will be installed using this installer's pattern and commands.
	// Either InstallerRef or RuntimeRef must be specified (mutually exclusive).
	InstallerRef string `json:"installerRef,omitempty"`

	// RepositoryRef references an InstallerRepository resource for all tools in this set.
	// Optional. When set, all tools will depend on this repository being configured first.
	RepositoryRef string `json:"repositoryRef,omitempty"`

	// RuntimeRef references a Runtime resource for installation.
	// When set, all tools in this set will be installed via the runtime's commands.install.
	// Either InstallerRef or RuntimeRef must be specified (mutually exclusive).
	RuntimeRef string `json:"runtimeRef,omitempty"`

	// Tools maps tool names to their individual configurations.
	// The key becomes the tool name (and typically the binary name).
	// Each tool can override version and source settings.
	Tools map[string]ToolItem `json:"tools"`
}

// Validate validates the ToolSetSpec.
func (s *ToolSetSpec) Validate() error {
	// Either installerRef or runtimeRef must be specified (mutually exclusive)
	if s.InstallerRef == "" && s.RuntimeRef == "" {
		return fmt.Errorf("either installerRef or runtimeRef is required")
	}
	if s.InstallerRef != "" && s.RuntimeRef != "" {
		return fmt.Errorf("cannot specify both installerRef and runtimeRef")
	}
	if len(s.Tools) == 0 {
		return fmt.Errorf("at least one tool is required")
	}
	return nil
}

// Dependencies returns the resources this toolset depends on.
func (s *ToolSetSpec) Dependencies() []Ref {
	var deps []Ref
	if s.InstallerRef != "" {
		deps = append(deps, Ref{Kind: KindInstaller, Name: s.InstallerRef})
	}
	if s.RepositoryRef != "" {
		deps = append(deps, Ref{Kind: KindInstallerRepository, Name: s.RepositoryRef})
	}
	if s.RuntimeRef != "" {
		deps = append(deps, Ref{Kind: KindRuntime, Name: s.RuntimeRef})
	}
	return deps
}

// ToolSet is a concrete resource type for tool sets.
type ToolSet struct {
	BaseResource
	ToolSetSpec *ToolSetSpec `json:"spec"`
}

// Kind returns the resource kind (can be called on nil).
func (*ToolSet) Kind() Kind { return KindToolSet }

// Spec returns the spec as Spec interface.
func (t *ToolSet) Spec() Spec { return t.ToolSetSpec }

// Expand expands a ToolSet into individual Tool resources.
// Disabled tools are excluded from the result.
func (ts *ToolSet) Expand() ([]Resource, error) {
	var tools []Resource
	for name, item := range ts.ToolSetSpec.Tools {
		if !item.IsEnabled() {
			continue
		}
		tools = append(tools, buildToolFromSetItem(ts, name, item))
	}
	return tools, nil
}

// buildToolFromSetItem creates a Tool resource from a ToolSet item.
func buildToolFromSetItem(ts *ToolSet, name string, item ToolItem) *Tool {
	return &Tool{
		BaseResource: BaseResource{
			APIVersion:   GroupVersion,
			ResourceKind: KindTool,
			Metadata:     Metadata{Name: name},
		},
		ToolSpec: &ToolSpec{
			InstallerRef:  ts.ToolSetSpec.InstallerRef,
			RepositoryRef: ts.ToolSetSpec.RepositoryRef,
			RuntimeRef:    ts.ToolSetSpec.RuntimeRef,
			Version:       item.Version,
			SHA:           item.SHA,
			Source:        item.Source,
			Package:       item.Package,
			BinaryName:    item.BinaryName,
			Args:          item.Args,
			Privileged:    item.Privileged,
		},
	}
}

// ToolItem represents a tool within a ToolSet.
// It provides per-tool overrides for version and source configuration.
type ToolItem struct {
	// Version specifies the tool version to install.
	// Overrides any default version from the ToolSet.
	Version string `json:"version,omitempty"`

	// SHA pins this tool to a git commit SHA. Mutually exclusive with Version.
	// See ToolSpec.SHA for full semantics. Currently supported only with
	// runtimeRef: "go".
	SHA string `json:"sha,omitempty"`

	// Enabled controls whether this specific tool should be installed.
	// Default is true. Set to false to exclude this tool from the set.
	Enabled *bool `json:"enabled,omitempty"`

	// Source provides download configuration for this specific tool.
	// Mutually exclusive with Package.
	Source *DownloadSource `json:"source,omitempty"`

	// Package specifies the package identifier for this tool.
	// For registry-based: { owner: "cli", repo: "cli" }
	// For delegation-based: { name: "golang.org/x/tools/gopls" }
	// Mutually exclusive with Source.
	Package *Package `json:"package,omitempty"`

	// BinaryName overrides the binary name for placement and symlink.
	BinaryName string `json:"binaryName,omitempty"`

	// Args provides additional arguments appended to the install command.
	// These are joined with spaces and available as {{.Args}} in command templates.
	Args []string `json:"args,omitempty"`

	// Privileged declares that this tool's commands require a cached sudo
	// timestamp while they run (i.e. --system is required to install/remove).
	// See ToolSpec.Privileged for full semantics. Default is false.
	Privileged bool `json:"privileged,omitempty"`
}

// UnmarshalJSON handles CUE's MarshalJSON quirk where single-element lists
// are serialized as bare strings for the Args field.
func (t *ToolItem) UnmarshalJSON(data []byte) error {
	type Alias ToolItem
	var r struct {
		Alias
		Args json.RawMessage `json:"args,omitempty"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	*t = ToolItem(r.Alias)
	return unmarshalStringFields([]stringField{
		{"args", r.Args, &t.Args},
	})
}

// IsEnabled returns whether the tool item is enabled.
func (t *ToolItem) IsEnabled() bool {
	if t.Enabled == nil {
		return true
	}
	return *t.Enabled
}

// ToolState represents the persisted state of an installed tool.
// This is stored in state.json and used for reconciliation to determine
// what actions are needed (install, upgrade, reinstall, remove).
type ToolState struct {
	// InstallerRef is the installer that was used to install this tool.
	InstallerRef string `json:"installerRef"`

	// RepositoryRef is the installer repository used for this tool (if any).
	RepositoryRef string `json:"repositoryRef,omitempty"`

	// Version is the installed version of the tool.
	Version string `json:"version"`

	// SHA records the 40-char commit SHA-1 that pinned this install (when
	// spec.sha was set). Mutually exclusive with Version in the spec; only one
	// of the two is populated at a time. Empty for tools installed by
	// tag/version (the common case).
	SHA string `json:"sha,omitempty"`

	// Digest is the SHA256 hash of the installed binary (for download pattern).
	// Used to verify integrity and detect if the binary was modified.
	Digest checksum.Digest `json:"digest,omitempty"`

	// InstallPath is the absolute path to the installed binary.
	// For download pattern: ~/.local/share/tomei/tools/{name}/{version}/{binary}
	// For delegation pattern: depends on the installer (e.g., ~/go/bin/{name})
	InstallPath string `json:"installPath"`

	// BinPath is the absolute path to the symlink in the user's bin directory.
	// Typically ~/.local/bin/{name} for download pattern tools.
	// May be empty for delegation pattern tools that manage their own PATH.
	BinPath string `json:"binPath"`

	// Source records the download configuration used for installation.
	// Stored for reference and potential re-download if needed.
	Source *DownloadSource `json:"source,omitempty"`

	// Package records the package identifier used for installation.
	// For registry-based: { owner: "cli", repo: "cli" }
	// For delegation-based: { name: "golang.org/x/tools/gopls" }
	Package *Package `json:"package,omitempty"`

	// RuntimeRef records which runtime was used for delegation installation.
	// Used to determine if the tool needs reinstallation when the runtime is upgraded.
	RuntimeRef string `json:"runtimeRef,omitempty"`

	// VersionKind classifies how the version was specified in the manifest.
	// Used by the reconciler to determine the correct comparison strategy.
	VersionKind VersionKind `json:"versionKind"`

	// SpecVersion records the original version specified in the spec.
	// For VersionExact: same as Version.
	// For VersionLatest: empty string.
	// For VersionAlias: the alias string (e.g., "stable").
	SpecVersion string `json:"specVersion,omitempty"`

	// Commands records the shell commands for self-managed tools.
	// Persisted in state so Remove() and Update can execute without re-reading the manifest.
	Commands *ToolCommandSet `json:"commands,omitempty"`

	// BinaryName records the user-specified binary name override from the spec.
	// Empty string means no override was provided and the installer used its default
	// effective name (e.g., tool name, registry files[].name, or delegation output).
	// Used by the reconciler to detect binaryName changes (both setting and unsetting).
	BinaryName string `json:"binaryName,omitempty"`

	// Privileged indicates this tool was installed under --system (its commands
	// expect a cached sudo timestamp while they run). Persisted so removal and
	// reconciliation can apply the same --system gate even when the manifest is
	// no longer present.
	//
	// Migration note: in the short-lived pre-fix semantics, `privileged: true`
	// meant tomei wrapped the whole command in "sudo -n sh -c ...". A state
	// entry carrying Commands.Remove written under those semantics may have a
	// bare "foo" where the new semantics need "sudo foo". Re-applying the
	// manifest updates the persisted commands, so the resolution is to update
	// the manifest (prefix root-requiring steps with "sudo ") and run
	// `tomei apply --system` to refresh state before triggering removal.
	Privileged bool `json:"privileged,omitempty"`

	// BinDirKind records which bin directory this tool's symlink lives in
	// ("user" or "system"). Empty in pre-SUB6 state files; read via
	// BinDirKindOrDefault, which treats empty as user. Privileged download/
	// registry tools (routed by SUB5) will carry "system" here.
	//
	// Set independently of Privileged: SUB5's routing decides the bin dir,
	// and Privileged tracks sudo-wrapping of commands. Do not derive one
	// from the other.
	BinDirKind BinDirKind `json:"binDirKind,omitempty"`

	// TaintReason indicates why this tool needs reinstallation.
	// Empty string means the tool is not tainted.
	TaintReason TaintReason `json:"taintReason,omitempty"`

	// UpdatedAt is the timestamp when this tool was last installed or updated.
	UpdatedAt time.Time `json:"updatedAt"`
}

func (*ToolState) isState() {}

// GetBinPath returns the symlink path for this tool.
// Nil-safe: returns empty string if receiver is nil.
func (t *ToolState) GetBinPath() string {
	if t == nil {
		return ""
	}
	return t.BinPath
}

// BinDirKindOrDefault returns the tool's bin-directory classification,
// treating an empty value (pre-SUB6 state files, or a nil receiver) as
// BinDirKindUser. The getter does not validate non-empty values: an unknown
// kind passes through unchanged.
func (t *ToolState) BinDirKindOrDefault() BinDirKind {
	if t == nil || t.BinDirKind == "" {
		return BinDirKindUser
	}
	return t.BinDirKind
}

// PrivilegedRemovalReason returns a short reason a privileged removal needs
// --system, derived from persisted state (the manifest may be absent). Used
// at the engine-side state-driven removal-skip log site, which guards on
// state.Privileged before invoking this method.
//
// State-side counterpart to Tool.PrivilegedReason (which works from the spec).
// Commands takes precedence: the sudo cache for shell commands is the more
// concrete user-visible action. Otherwise, BinDirKindSystem indicates the
// system-bin-dir cleanup case (SUB5+). When state.Privileged is true but
// neither indicator pinpoints the cause (e.g., a pre-SUB5 privileged
// download/registry install — Privileged is stamped but BinDirKind is still
// user), fall back to a generic non-empty reason so the log never carries
// reason="".
func (t *ToolState) PrivilegedRemovalReason() string {
	if t == nil {
		return ""
	}
	if t.Commands != nil {
		return PrivilegedRemovalReasonCommands
	}
	if t.BinDirKindOrDefault() == BinDirKindSystem {
		return PrivilegedRemovalReasonSystemBinDir
	}
	return PrivilegedRemovalReasonGeneric
}

// Reason strings returned by ToolState.PrivilegedRemovalReason. Exported so
// tests and downstream slog consumers can reference them without duplication.
const (
	// PrivilegedRemovalReasonCommands: state carries Commands (Commands-pattern
	// privileged tool); removal will run sudo-backed shell commands.
	PrivilegedRemovalReasonCommands = "shell command removal"
	// PrivilegedRemovalReasonSystemBinDir: state's BinDirKind is system
	// (download/registry tool placed in SystemBinDir, SUB5+).
	PrivilegedRemovalReasonSystemBinDir = "system bin directory cleanup"
	// PrivilegedRemovalReasonGeneric: state.Privileged is true but neither
	// indicator pinpoints the cause (e.g., a pre-SUB5 privileged download or
	// registry install — Privileged is stamped but BinDirKind is still user).
	PrivilegedRemovalReasonGeneric = "privileged tool removal"
)

// IsTainted returns true if the tool needs reinstallation.
func (t *ToolState) IsTainted() bool {
	return t.TaintReason != ""
}

// Taint marks the tool for reinstallation.
func (t *ToolState) Taint(reason TaintReason) {
	t.TaintReason = reason
}

// ClearTaint removes the taint flag.
func (t *ToolState) ClearTaint() {
	t.TaintReason = ""
}
