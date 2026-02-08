package resource

import (
	"fmt"
	"time"
)

// InstallerRepositorySourceType represents the type of repository source.
type InstallerRepositorySourceType string

const (
	// InstallerRepositorySourceDelegation means the installer manages its own repos
	// via shell commands (e.g., helm repo add, kubectl krew index add).
	InstallerRepositorySourceDelegation InstallerRepositorySourceType = "delegation"

	// InstallerRepositorySourceGit means tomei clones/manages a git repository
	// (e.g., aqua custom registries).
	InstallerRepositorySourceGit InstallerRepositorySourceType = "git"
)

// InstallerRepositorySourceSpec defines the source configuration for a repository.
type InstallerRepositorySourceSpec struct {
	// Type specifies how the repository is managed.
	// "delegation": uses Commands to add/check/remove the repo via the installer CLI.
	// "git": tomei clones a git repository to a local path.
	Type InstallerRepositorySourceType `json:"type"`

	// URL is the repository URL.
	// For git: the git clone URL (must be HTTPS).
	URL string `json:"url,omitempty"`

	// Commands defines shell commands for delegation-type repositories.
	// Required when Type is "delegation".
	Commands *CommandSet `json:"commands,omitempty"`
}

// InstallerRepositorySpec defines a third-party repository for an installer.
type InstallerRepositorySpec struct {
	// InstallerRef references the Installer resource this repository belongs to.
	// The installer must exist and be available before this repository can be configured.
	InstallerRef string `json:"installerRef"`

	// Source configures how the repository is set up.
	Source InstallerRepositorySourceSpec `json:"source"`
}

// Validate validates the InstallerRepositorySpec.
func (s *InstallerRepositorySpec) Validate() error {
	if s.InstallerRef == "" {
		return fmt.Errorf("installerRef is required")
	}
	if s.Source.Type == "" {
		return fmt.Errorf("source.type is required")
	}
	switch s.Source.Type {
	case InstallerRepositorySourceDelegation:
		if s.Source.Commands == nil || s.Source.Commands.Install == "" {
			return fmt.Errorf("source.commands.install is required for delegation type")
		}
	case InstallerRepositorySourceGit:
		if s.Source.URL == "" {
			return fmt.Errorf("source.url is required for git type")
		}
	default:
		return fmt.Errorf("source.type must be 'delegation' or 'git', got %q", s.Source.Type)
	}
	return nil
}

// Dependencies returns the resources this repository depends on.
func (s *InstallerRepositorySpec) Dependencies() []Ref {
	return []Ref{
		{Kind: KindInstaller, Name: s.InstallerRef},
	}
}

// InstallerRepository is a concrete resource type for installer repositories.
type InstallerRepository struct {
	BaseResource
	InstallerRepositorySpec *InstallerRepositorySpec `json:"spec"`
}

// Kind returns the resource kind (can be called on nil).
func (*InstallerRepository) Kind() Kind { return KindInstallerRepository }

// Spec returns the spec as Spec interface.
func (r *InstallerRepository) Spec() Spec { return r.InstallerRepositorySpec }

// InstallerRepositoryState represents the persisted state of an installer repository.
type InstallerRepositoryState struct {
	// InstallerRef is the installer this repository belongs to.
	InstallerRef string `json:"installerRef"`

	// SourceType records which source type was used.
	SourceType InstallerRepositorySourceType `json:"sourceType"`

	// URL records the repository URL.
	URL string `json:"url,omitempty"`

	// LocalPath records where a git-type repository was cloned to.
	// Empty for delegation type.
	LocalPath string `json:"localPath,omitempty"`

	// RemoveCommand stores the remove command for delegation type.
	// Stored in state because Remove() only receives state (no spec).
	RemoveCommand string `json:"removeCommand,omitempty"`

	// UpdatedAt is the timestamp when this repository was last configured.
	UpdatedAt time.Time `json:"updatedAt"`
}

func (*InstallerRepositoryState) isState() {}
