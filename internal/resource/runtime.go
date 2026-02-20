package resource

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/terassyi/tomei/internal/checksum"
)

// RuntimeSpec defines a language runtime (e.g., Go, Rust, Node.js).
// Runtimes are foundational resources that provide:
//   - The runtime binaries themselves (e.g., go, gofmt, rustc, cargo)
//   - A way to install tools via the runtime's package manager (e.g., go install, cargo install)
//   - Environment variables needed for the runtime to function
//
// Runtimes support two installation types:
//   - Download: tomei downloads and extracts a tarball (e.g., Go)
//   - Delegation: tomei executes an external installer script (e.g., Rust via rustup)
type RuntimeSpec struct {
	// Type specifies how this runtime is installed.
	// Must be either "download" or "delegation".
	Type InstallType `json:"type"`

	// Version specifies the runtime version to install.
	// Can be a specific version (e.g., "1.25.1") or an alias ("stable", "latest").
	// When using an alias, Bootstrap.ResolveVersion is used to resolve the actual version.
	Version string `json:"version"`

	// Source configures where to download the runtime from.
	// Required for download pattern. Not used for delegation pattern.
	Source *DownloadSource `json:"source,omitempty"`

	// Bootstrap configures how to install the runtime itself.
	// Required for delegation pattern. Not used for download pattern.
	Bootstrap *RuntimeBootstrapSpec `json:"bootstrap,omitempty"`

	// Binaries lists the executable names provided by this runtime.
	// These binaries will be symlinked to BinDir for PATH access.
	// Optional: if empty, tomei auto-detects executables in the bin directory.
	// Example for Go: ["go", "gofmt"]
	// Example for Rust: ["rustc", "cargo", "rustup"]
	Binaries []string `json:"binaries,omitempty"`

	// BinDir specifies where runtime binaries are located.
	// For download pattern: defaults to "{{.InstallPath}}/bin"
	// For delegation pattern: defaults to ToolBinPath
	// Example: "~/.cargo/bin" for Rust
	BinDir string `json:"binDir,omitempty"`

	// ToolBinPath specifies where tools installed via this runtime should be placed.
	// This is where "go install" or "cargo install" will put binaries.
	// Example: "~/go/bin" for Go tools, "~/.cargo/bin" for Rust tools.
	ToolBinPath string `json:"toolBinPath"`

	// Commands defines shell commands for installing TOOLS via this runtime.
	// This is NOT for installing the runtime itself, but for installing tools
	// using the runtime's package manager (e.g., go install, cargo install).
	// Used when a Tool has RuntimeRef pointing to this runtime.
	// Example for Go: Install: "go install {{.Package}}@{{.Version}}"
	Commands *CommandsSpec `json:"commands,omitempty"`

	// Env defines environment variables required by the runtime.
	// Supports template variable {{.InstallPath}} for the runtime installation directory.
	// Example for Go: {"GOROOT": "{{.InstallPath}}", "GOBIN": "~/go/bin"}
	Env map[string]string `json:"env,omitempty"`

	// TaintOnUpgrade controls whether dependent tools are tainted (marked for reinstall)
	// when this runtime is upgraded. When true, all tools with RuntimeRef pointing to
	// this runtime will be reinstalled after a runtime version change.
	// Default is false (opt-in). Presets for Go and Rust set this to true.
	TaintOnUpgrade bool `json:"taintOnUpgrade,omitempty"`

	// ResolveVersion is an optional command to resolve version aliases for download-pattern runtimes.
	// When set, the command is executed to resolve the actual version before downloading.
	// Supports a built-in "github-release:owner/repo:tagPrefix" syntax for fetching
	// the latest GitHub release tag, or arbitrary shell commands.
	// Example: ["github-release:oven-sh/bun:bun-v"]
	// Example: ["curl -sL https://go.dev/VERSION?m=text | head -1 | sed 's/^go//'"]
	ResolveVersion []string `json:"resolveVersion,omitempty"`
}

// UnmarshalJSON handles CUE's MarshalJSON quirk where single-element lists
// are serialized as bare strings for the Binaries field.
func (s *RuntimeSpec) UnmarshalJSON(data []byte) error {
	type Alias RuntimeSpec
	var r struct {
		Alias
		Binaries       json.RawMessage `json:"binaries,omitempty"`
		ResolveVersion json.RawMessage `json:"resolveVersion,omitempty"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	*s = RuntimeSpec(r.Alias)
	return unmarshalStringFields([]stringField{
		{"binaries", r.Binaries, &s.Binaries},
		{"resolveVersion", r.ResolveVersion, &s.ResolveVersion},
	})
}

// RuntimeBootstrapSpec defines how to install the runtime itself (delegation pattern).
// It embeds CommandSet for install/check/remove and adds runtime-specific fields.
//
// Example for Rust:
//   - Install: "curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain {{.Version}}"
//   - Check: "rustc --version"
//   - Remove: "rustup self uninstall -y"
type RuntimeBootstrapSpec struct {
	CommandSet

	// Update is an optional command to update the runtime in-place (e.g., "rustup update stable").
	// When set and the action is upgrade or reinstall, this command is used instead of Install.
	// This avoids re-running the full bootstrap installer for lightweight updates.
	Update []string `json:"update,omitempty"`

	// ResolveVersion is an optional command to resolve version aliases like "stable" or "latest".
	// Should output the actual version number to stdout.
	// Example: ["rustup check 2>/dev/null | grep -oP 'stable-.*?: \\K[0-9.]+' || echo ''"]
	ResolveVersion []string `json:"resolveVersion,omitempty"`
}

// UnmarshalJSON handles CUE's MarshalJSON quirk where single-element lists
// are serialized as bare strings. Delegates Install/Check/Remove to CommandSet
// and handles ResolveVersion separately.
func (r *RuntimeBootstrapSpec) UnmarshalJSON(data []byte) error {
	// Decode the embedded CommandSet fields (Install, Check, Remove).
	if err := r.CommandSet.UnmarshalJSON(data); err != nil {
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
		{"update", extra.Update, &r.Update},
		{"resolveVersion", extra.ResolveVersion, &r.ResolveVersion},
	})
}

// Validate validates the RuntimeSpec.
func (s *RuntimeSpec) Validate() error {
	if s.Version == "" {
		return fmt.Errorf("version is required")
	}
	if s.ToolBinPath == "" {
		return fmt.Errorf("toolBinPath is required")
	}

	// Type-specific validation
	if s.Type.IsDownload() {
		if s.Source == nil || s.Source.URL == "" {
			return fmt.Errorf("source.url is required for download type")
		}
	}
	if s.Type.IsDelegation() {
		if s.Bootstrap == nil {
			return fmt.Errorf("bootstrap is required for delegation type")
		}
		if len(s.Bootstrap.Install) == 0 {
			return fmt.Errorf("bootstrap.install is required for delegation type")
		}
		if len(s.Bootstrap.Check) == 0 {
			return fmt.Errorf("bootstrap.check is required for delegation type")
		}
	}

	return nil
}

// Dependencies returns the resources this runtime depends on.
func (s *RuntimeSpec) Dependencies() []Ref {
	// Runtime has no dependencies (it's a base resource)
	return nil
}

// Runtime is a concrete resource type for language runtimes.
type Runtime struct {
	BaseResource
	RuntimeSpec *RuntimeSpec `json:"spec"`
}

// Kind returns the resource kind (can be called on nil).
func (*Runtime) Kind() Kind { return KindRuntime }

// Spec returns the spec as Spec interface.
func (r *Runtime) Spec() Spec { return r.RuntimeSpec }

// RuntimeState represents the persisted state of an installed runtime.
// This is stored in state.json and used for reconciliation and taint detection.
type RuntimeState struct {
	// Type records which installation type was used.
	Type InstallType `json:"type"`

	// Version is the installed version of the runtime.
	// This is the actual resolved version, not an alias like "stable".
	Version string `json:"version"`

	// VersionKind classifies how the version was specified in the manifest.
	// Used by the reconciler to determine the correct comparison strategy.
	VersionKind VersionKind `json:"versionKind"`

	// SpecVersion records the original version specified in the spec.
	// For VersionExact: same as Version.
	// For VersionLatest: empty string.
	// For VersionAlias: the alias string (e.g., "stable").
	SpecVersion string `json:"specVersion,omitempty"`

	// Digest is the SHA256 hash of the downloaded archive (download pattern only).
	// Used to verify integrity and detect corruption.
	Digest checksum.Digest `json:"digest,omitempty"`

	// InstallPath is the absolute path where the runtime is installed.
	// For download pattern: ~/.local/share/tomei/runtimes/go/1.25.1
	// For delegation pattern: may be empty (managed by external tool)
	InstallPath string `json:"installPath,omitempty"`

	// Binaries lists the installed executable names.
	// Stored for cleanup when the runtime is removed.
	Binaries []string `json:"binaries,omitempty"`

	// BinDir records where runtime binaries are located.
	// Used for cleanup and to locate runtime binaries.
	BinDir string `json:"binDir,omitempty"`

	// ToolBinPath records where tools are installed via this runtime.
	// Used by tools that reference this runtime.
	ToolBinPath string `json:"toolBinPath"`

	// Commands records the install commands for tools.
	// Used when a Tool has RuntimeRef pointing to this runtime.
	Commands *CommandsSpec `json:"commands,omitempty"`

	// Env records the environment variables configured for this runtime.
	// Used when executing tools that depend on this runtime.
	Env map[string]string `json:"env,omitempty"`

	// RemoveCommand is the shell command(s) to uninstall a delegation-pattern runtime.
	// Stored in state because Remove() only receives state (no spec).
	RemoveCommand []string `json:"removeCommand,omitempty"`

	// TaintOnUpgrade records whether dependent tools should be tainted on runtime upgrade.
	// Propagated from RuntimeSpec during installation.
	TaintOnUpgrade bool `json:"taintOnUpgrade,omitempty"`

	// TaintReason indicates why this runtime needs reinstallation.
	// Common reasons: "update_requested" (--update-runtimes flag).
	// Empty string means the runtime is not tainted.
	TaintReason string `json:"taintReason,omitempty"`

	// UpdatedAt is the timestamp when this runtime was last installed or updated.
	UpdatedAt time.Time `json:"updatedAt"`
}

func (*RuntimeState) isState() {}

// IsTainted returns true if the runtime needs reinstallation.
func (s *RuntimeState) IsTainted() bool {
	return s.TaintReason != ""
}

// Taint marks the runtime for reinstallation.
func (s *RuntimeState) Taint(reason string) {
	s.TaintReason = reason
}

// ClearTaint removes the taint flag.
func (s *RuntimeState) ClearTaint() {
	s.TaintReason = ""
}
