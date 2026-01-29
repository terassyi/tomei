package resource

import (
	"fmt"
	"time"
)

// InstallerPattern represents the type of installer.
type InstallerPattern string

const (
	InstallerPatternDownload   InstallerPattern = "download"
	InstallerPatternDelegation InstallerPattern = "delegation"
)

// InstallerSpec defines a user-level installer.
type InstallerSpec struct {
	Pattern    InstallerPattern `json:"pattern"` // "download" or "delegation"
	RuntimeRef string           `json:"runtimeRef,omitempty"`
	Bootstrap  *BootstrapSpec   `json:"bootstrap,omitempty"`
	Commands   *CommandsSpec    `json:"commands,omitempty"`
}

// Validate validates the InstallerSpec.
func (s *InstallerSpec) Validate() error {
	if s.Pattern == "" {
		return fmt.Errorf("pattern is required")
	}
	if s.Pattern != InstallerPatternDownload && s.Pattern != InstallerPatternDelegation {
		return fmt.Errorf("pattern must be 'download' or 'delegation'")
	}
	if s.Pattern == InstallerPatternDelegation && s.Commands == nil {
		return fmt.Errorf("commands is required for delegation pattern")
	}
	return nil
}

// Dependencies returns the resources this installer depends on.
func (s *InstallerSpec) Dependencies() []ResourceRef {
	if s.RuntimeRef != "" {
		return []ResourceRef{{Kind: KindRuntime, Name: s.RuntimeRef}}
	}
	return nil
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

// BootstrapSpec defines how to install the installer itself.
type BootstrapSpec struct {
	Install string `json:"install"`          // Command to install the installer
	Check   string `json:"check"`            // Command to check if installer exists
	Remove  string `json:"remove,omitempty"` // Command to remove the installer
}

// CommandsSpec defines commands for a delegation pattern installer.
type CommandsSpec struct {
	Install string `json:"install"`
	Check   string `json:"check,omitempty"`
	Remove  string `json:"remove,omitempty"`
}

// InstallerState represents the state of an installer.
type InstallerState struct {
	Version   string    `json:"version"`
	UpdatedAt time.Time `json:"updatedAt"`
}
