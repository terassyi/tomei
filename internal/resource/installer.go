package resource

import (
	"fmt"
	"time"
)

// InstallerSpec defines a user-level installer that can install tools.
// Installers come in two patterns:
//   - Download: toto directly downloads and places binaries (e.g., aqua for GitHub releases)
//   - Delegation: toto delegates to external package managers (e.g., brew, cargo binstall)
//
// Note: For tools installed via a Runtime (e.g., go install, cargo install),
// use Tool.RuntimeRef directly instead of creating an Installer.
// Installers are only needed for:
//   - Tools without a Runtime (aqua, brew)
//   - Installers that depend on a Runtime (go installer depends on go runtime)
//   - Installers that depend on a Tool (binstall depends on cargo-binstall)
type InstallerSpec struct {
	// Type specifies how this installer installs tools.
	// Must be either "download" or "delegation".
	Type InstallType `json:"type"`

	// RuntimeRef references a Runtime resource that this installer depends on.
	// Used when the installer requires a runtime to function (e.g., go installer needs go runtime).
	// The referenced runtime must be installed before this installer can be used.
	// Cannot be specified together with ToolRef.
	RuntimeRef string `json:"runtimeRef,omitempty"`

	// ToolRef references a Tool resource that this installer depends on.
	// Enables tool-as-installer chains (e.g., cargo-binstall tool -> binstall installer -> ripgrep tool).
	// The referenced tool must be installed before this installer can be used.
	// Cannot be specified together with RuntimeRef.
	ToolRef string `json:"toolRef,omitempty"`

	// Bootstrap configures how to install the installer itself (self-installation).
	// Used for installers that are not provided by a runtime or tool.
	// Example: Homebrew installs itself via a shell script.
	Bootstrap *BootstrapSpec `json:"bootstrap,omitempty"`

	// Commands defines the shell commands for delegation pattern installers.
	// Required when Pattern is "delegation".
	// Supports template variables: {{.Package}}, {{.Version}}, {{.Name}}, {{.BinPath}}.
	Commands *CommandsSpec `json:"commands,omitempty"`
}

// Validate validates the InstallerSpec.
func (s *InstallerSpec) Validate() error {
	// Type is required
	if s.Type == "" {
		return fmt.Errorf("type is required")
	}

	// Type must be valid
	if !s.Type.IsDownload() && !s.Type.IsDelegation() {
		return fmt.Errorf("type must be 'download' or 'delegation', got %q", s.Type)
	}

	// RuntimeRef and ToolRef are mutually exclusive
	if s.RuntimeRef != "" && s.ToolRef != "" {
		return fmt.Errorf("cannot specify both runtimeRef and toolRef")
	}

	// Delegation type requires Commands
	if s.Type.IsDelegation() && s.Commands == nil {
		return fmt.Errorf("commands is required for delegation type")
	}

	return nil
}

// Dependencies returns the resources this installer depends on.
func (s *InstallerSpec) Dependencies() []Ref {
	var deps []Ref
	if s.RuntimeRef != "" {
		deps = append(deps, Ref{Kind: KindRuntime, Name: s.RuntimeRef})
	}
	if s.ToolRef != "" {
		deps = append(deps, Ref{Kind: KindTool, Name: s.ToolRef})
	}
	return deps
}

// Installer is a concrete resource type for user-level installers.
type Installer struct {
	BaseResource
	InstallerSpec *InstallerSpec `json:"spec"`
}

// Kind returns the resource kind (can be called on nil).
func (*Installer) Kind() Kind { return KindInstaller }

// Spec returns the spec as Spec interface.
func (i *Installer) Spec() Spec { return i.InstallerSpec }

// BootstrapSpec defines how to install the installer itself (self-installation).
// This is used for installers like Homebrew that need to be installed before they can install tools.
//
// Example for Homebrew:
//   - Install: "/bin/bash -c \"$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)\""
//   - Check: "command -v brew"
//   - Remove: "/bin/bash -c \"$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/uninstall.sh)\""
type BootstrapSpec = CommandSet

// CommandsSpec defines the shell commands for a delegation type installer.
// Commands support Go template variables for dynamic substitution:
//   - {{.Package}}: The package identifier (e.g., "golang.org/x/tools/gopls")
//   - {{.Version}}: The version to install (e.g., "v0.17.1")
//   - {{.Name}}: The tool name (e.g., "gopls")
//   - {{.BinPath}}: The expected binary path after installation
//
// Example for go install:
//   - Install: "go install {{.Package}}@{{.Version}}"
//   - Check: "go version -m {{.BinPath}}"
//   - Remove: "rm {{.BinPath}}"
type CommandsSpec = CommandSet

// InstallerState represents the persisted state of an installer.
// Currently minimal as installers themselves don't have much state to track.
type InstallerState struct {
	// Version tracks the installer version (if applicable).
	Version string `json:"version"`

	// UpdatedAt is the timestamp when this installer was last configured.
	UpdatedAt time.Time `json:"updatedAt"`
}

func (*InstallerState) isState() {}
