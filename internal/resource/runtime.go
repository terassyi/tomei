package resource

import (
	"fmt"
	"time"
)

// RuntimeSpec defines a language runtime.
type RuntimeSpec struct {
	InstallerRef string            `json:"installerRef"`
	Version      string            `json:"version"`
	Source       DownloadSource    `json:"source"`
	Binaries     []string          `json:"binaries"`
	ToolBinPath  string            `json:"toolBinPath"`
	Env          map[string]string `json:"env,omitempty"`
}

// Validate validates the RuntimeSpec.
func (s *RuntimeSpec) Validate() error {
	if s.Version == "" {
		return fmt.Errorf("version is required")
	}
	if s.Source.URL == "" {
		return fmt.Errorf("source.url is required")
	}
	return nil
}

// Dependencies returns the resources this runtime depends on.
func (s *RuntimeSpec) Dependencies() []ResourceRef {
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

// RuntimeState represents the state of an installed runtime.
type RuntimeState struct {
	InstallerRef string            `json:"installerRef"`
	Version      string            `json:"version"`
	Digest       string            `json:"digest,omitempty"`
	InstallPath  string            `json:"installPath"`
	Binaries     []string          `json:"binaries"`
	ToolBinPath  string            `json:"toolBinPath"`
	Env          map[string]string `json:"env,omitempty"`
	UpdatedAt    time.Time         `json:"updatedAt"`
}
