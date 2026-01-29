package resource

import (
	"fmt"
	"time"
)

// DownloadSource holds download configuration.
type DownloadSource struct {
	URL         string `json:"url"`
	Checksum    string `json:"checksum,omitempty"`
	ArchiveType string `json:"archiveType,omitempty"`
}

// ToolSpec defines an individual tool.
type ToolSpec struct {
	InstallerRef string          `json:"installerRef"`
	Version      string          `json:"version"`
	Enabled      *bool           `json:"enabled,omitempty"` // default: true
	Source       *DownloadSource `json:"source,omitempty"`  // for download pattern
	RuntimeRef   string          `json:"runtimeRef,omitempty"`
	Package      string          `json:"package,omitempty"` // for runtime delegation
}

// Validate validates the ToolSpec.
func (s *ToolSpec) Validate() error {
	if s.InstallerRef == "" {
		return fmt.Errorf("installerRef is required")
	}
	if s.Version == "" && s.Package == "" {
		return fmt.Errorf("version or package is required")
	}
	return nil
}

// Dependencies returns the resources this tool depends on.
func (s *ToolSpec) Dependencies() []Ref {
	deps := []Ref{
		{Kind: KindInstaller, Name: s.InstallerRef},
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

// ToolSetSpec defines a set of tools.
type ToolSetSpec struct {
	InstallerRef string              `json:"installerRef"`
	RuntimeRef   string              `json:"runtimeRef,omitempty"`
	Tools        map[string]ToolItem `json:"tools"`
}

// Validate validates the ToolSetSpec.
func (s *ToolSetSpec) Validate() error {
	if s.InstallerRef == "" {
		return fmt.Errorf("installerRef is required")
	}
	if len(s.Tools) == 0 {
		return fmt.Errorf("at least one tool is required")
	}
	return nil
}

// Dependencies returns the resources this toolset depends on.
func (s *ToolSetSpec) Dependencies() []Ref {
	deps := []Ref{
		{Kind: KindInstaller, Name: s.InstallerRef},
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

// ToolItem represents a tool within a ToolSet.
type ToolItem struct {
	Version string          `json:"version,omitempty"`
	Enabled *bool           `json:"enabled,omitempty"`
	Source  *DownloadSource `json:"source,omitempty"`
	Package string          `json:"package,omitempty"`
}

// IsEnabled returns whether the tool item is enabled.
func (t *ToolItem) IsEnabled() bool {
	if t.Enabled == nil {
		return true
	}
	return *t.Enabled
}

// ToolState represents the state of an installed tool.
type ToolState struct {
	InstallerRef string          `json:"installerRef"`
	Version      string          `json:"version"`
	Digest       string          `json:"digest,omitempty"`
	InstallPath  string          `json:"installPath"`
	BinPath      string          `json:"binPath"`
	Source       *DownloadSource `json:"source,omitempty"`
	RuntimeRef   string          `json:"runtimeRef,omitempty"`
	Package      string          `json:"package,omitempty"`
	TaintReason  string          `json:"taintReason,omitempty"`
	UpdatedAt    time.Time       `json:"updatedAt"`
}

// IsTainted returns true if the tool needs reinstallation.
func (t *ToolState) IsTainted() bool {
	return t.TaintReason != ""
}

// Taint marks the tool for reinstallation.
func (t *ToolState) Taint(reason string) {
	t.TaintReason = reason
}

// ClearTaint removes the taint flag.
func (t *ToolState) ClearTaint() {
	t.TaintReason = ""
}
