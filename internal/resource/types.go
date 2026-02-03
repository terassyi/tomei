package resource

// Kind represents the type of resource (K8s style).
type Kind string

const (
	// System privilege resources
	KindSystemInstaller         Kind = "SystemInstaller"
	KindSystemPackageRepository Kind = "SystemPackageRepository"
	KindSystemPackageSet        Kind = "SystemPackageSet"

	// User privilege resources
	KindInstaller Kind = "Installer"
	KindRuntime   Kind = "Runtime"
	KindTool      Kind = "Tool"
	KindToolSet   Kind = "ToolSet"
)

const (
	// ProjectName is the name of the project.
	ProjectName = "toto"

	// APIGroup is the API group for toto resources.
	APIGroup = "toto.terassyi.net"

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

// Resource is the interface that all toto resources must implement.
type Resource interface {
	Kind() Kind
	Name() string
	Labels() map[string]string
	Spec() Spec
}

// Metadata holds resource identification information.
type Metadata struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels,omitempty"`
}

// InstallType represents the installation method for resources (Runtime, Installer).
type InstallType string

const (
	// InstallTypeDownload indicates direct binary download.
	// toto handles downloading, extracting, and placing binaries.
	// Example: Go runtime from go.dev, aqua installer for GitHub releases.
	InstallTypeDownload InstallType = "download"

	// InstallTypeDelegation indicates delegation to external commands.
	// toto executes configured install/check/remove commands.
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

// CommandSet defines a set of shell commands for install/check/remove operations.
// This is the common type used by BootstrapSpec, CommandsSpec, and RuntimeBootstrapSpec.
// Commands may support Go template variables depending on the context.
type CommandSet struct {
	// Install is the shell command to install/setup.
	Install string `json:"install"`

	// Check is the shell command to verify installation.
	// Should exit 0 if installed, non-zero otherwise.
	Check string `json:"check,omitempty"`

	// Remove is the shell command to uninstall/cleanup.
	Remove string `json:"remove,omitempty"`
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
