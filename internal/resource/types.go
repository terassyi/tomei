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

// APIVersion is the current API version for toto resources.
const APIVersion = "toto.terassyi.net/v1beta1"

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
