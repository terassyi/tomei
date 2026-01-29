package resource

import (
	"fmt"
	"time"
)

// SystemInstallerSpec defines a package manager.
type SystemInstallerSpec struct {
	Pattern    string                      `json:"pattern"` // "delegation"
	Privileged bool                        `json:"privileged"`
	Commands   SystemInstallerCommandsSpec `json:"commands"`
}

// Validate validates the SystemInstallerSpec.
func (s *SystemInstallerSpec) Validate() error {
	if s.Pattern == "" {
		return fmt.Errorf("pattern is required")
	}
	return nil
}

// Dependencies returns the resources this system installer depends on.
func (s *SystemInstallerSpec) Dependencies() []Ref {
	// SystemInstaller has no dependencies (it's a base resource)
	return nil
}

// SystemInstaller is a concrete resource type for system-level installers.
type SystemInstaller struct {
	BaseResource
	SystemInstallerSpec *SystemInstallerSpec `json:"spec"`
}

// Kind returns the resource kind (can be called on nil).
func (*SystemInstaller) Kind() Kind { return KindSystemInstaller }

// Spec returns the spec as Spec interface.
func (s *SystemInstaller) Spec() Spec { return s.SystemInstallerSpec }

// SystemInstallerCommandsSpec defines commands for a system installer.
type SystemInstallerCommandsSpec struct {
	Install CommandSpec `json:"install"`
	Remove  CommandSpec `json:"remove"`
	Check   CommandSpec `json:"check"`
	Update  string      `json:"update,omitempty"`
}

// CommandSpec defines a command with its verb.
type CommandSpec struct {
	Command string `json:"command"`
	Verb    string `json:"verb"`
}

// SystemInstallerState represents the state of a system installer.
type SystemInstallerState struct {
	Version   string    `json:"version"`
	UpdatedAt time.Time `json:"updatedAt"`
}
