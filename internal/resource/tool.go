package resource

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/terassyi/tomei/internal/checksum"
	"github.com/terassyi/tomei/internal/installer/extract"
)

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
}

// ToolSpec defines the desired state of an individual tool.
// A tool can be installed via three patterns:
//  1. Download pattern (explicit): Downloads with Source specified
//  2. Download pattern (registry): Uses Package with owner/repo to resolve URL from aqua-registry
//  3. Delegation pattern: Uses Package with name for runtime/installer commands
type ToolSpec struct {
	// InstallerRef references an Installer resource by name.
	// For download pattern: points to an installer like "aqua" that handles downloading.
	// For delegation pattern: points to an installer like "go", "cargo", "npm" that has install commands.
	// Either InstallerRef or RuntimeRef must be specified.
	InstallerRef string `json:"installerRef,omitempty"`

	// RepositoryRef references an InstallerRepository resource by name.
	// Used when the tool is installed from a third-party repository
	// (e.g., a Helm chart from a custom repo, a krew plugin from a custom index).
	// The referenced InstallerRepository must be configured before this tool can be installed.
	RepositoryRef string `json:"repositoryRef,omitempty"`

	// Version specifies the tool version to install.
	// Format depends on the tool (e.g., "2.62.0", "v2.62.0", "latest").
	// Required for download pattern with Source; optional for registry pattern (defaults to latest).
	Version string `json:"version,omitempty"`

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
	// Either InstallerRef or RuntimeRef must be specified.
	RuntimeRef string `json:"runtimeRef,omitempty"`

	// Args provides additional arguments appended to the install command.
	// These are joined with spaces and available as {{.Args}} in command templates.
	// Example: ["--with-executables-from", "ansible-core"] for uv tool install.
	Args []string `json:"args,omitempty"`
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
	// Either installerRef or runtimeRef must be specified
	if s.InstallerRef == "" && s.RuntimeRef == "" {
		return fmt.Errorf("either installerRef or runtimeRef is required")
	}

	// Version, Source, or Package must be specified
	if s.Version == "" && s.Source == nil && s.Package.IsEmpty() {
		return fmt.Errorf("version, source, or package is required")
	}

	// Runtime delegation requires package
	if s.RuntimeRef != "" && s.Package.IsEmpty() {
		return fmt.Errorf("package is required when using runtimeRef")
	}

	// Source and Package are mutually exclusive
	if s.Source != nil && !s.Package.IsEmpty() {
		return fmt.Errorf("cannot specify both source and package")
	}

	// Registry package (explicit owner/repo object) requires InstallerRef="aqua"
	if s.Package.IsRegistry() && s.InstallerRef != "aqua" {
		return fmt.Errorf("package with owner/repo requires installerRef: aqua")
	}

	// Validate Package if specified
	if err := s.Package.Validate(); err != nil {
		return err
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
		tool := &Tool{
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
				Source:        item.Source,
				Package:       item.Package,
				Args:          item.Args,
			},
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

// ToolItem represents a tool within a ToolSet.
// It provides per-tool overrides for version and source configuration.
type ToolItem struct {
	// Version specifies the tool version to install.
	// Overrides any default version from the ToolSet.
	Version string `json:"version,omitempty"`

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

	// Args provides additional arguments appended to the install command.
	// These are joined with spaces and available as {{.Args}} in command templates.
	Args []string `json:"args,omitempty"`
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

	// TaintReason indicates why this tool needs reinstallation.
	// Empty string means the tool is not tainted.
	TaintReason TaintReason `json:"taintReason,omitempty"`

	// UpdatedAt is the timestamp when this tool was last installed or updated.
	UpdatedAt time.Time `json:"updatedAt"`
}

func (*ToolState) isState() {}

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
