package state

import (
	"time"

	"github.com/terassyi/toto/internal/resource"
)

// Version is the current state file format version.
const Version = "1"

// State is a constraint for state types that can be stored.
type State interface {
	UserState | SystemState
}

// RegistryState represents registry information stored in state.json.
type RegistryState struct {
	Aqua *AquaRegistryState `json:"aqua,omitempty"`
}

// AquaRegistryState represents the state of aqua-registry.
type AquaRegistryState struct {
	Ref       string    `json:"ref"` // e.g., "v4.465.0"
	UpdatedAt time.Time `json:"updatedAt"`
}

// UserState represents the state for user-privilege resources.
type UserState struct {
	Version    string                              `json:"version"`
	Registry   *RegistryState                      `json:"registry,omitempty"`
	Installers map[string]*resource.InstallerState `json:"installers,omitempty"`
	Runtimes   map[string]*resource.RuntimeState   `json:"runtimes,omitempty"`
	Tools      map[string]*resource.ToolState      `json:"tools,omitempty"`
}

// NewUserState creates a new empty UserState.
func NewUserState() *UserState {
	return &UserState{
		Version:    Version,
		Installers: make(map[string]*resource.InstallerState),
		Runtimes:   make(map[string]*resource.RuntimeState),
		Tools:      make(map[string]*resource.ToolState),
	}
}

// SystemState represents the state for system-privilege resources.
type SystemState struct {
	Version                   string                                            `json:"version"`
	SystemInstallers          map[string]*resource.SystemInstallerState         `json:"systemInstallers,omitempty"`
	SystemPackageRepositories map[string]*resource.SystemPackageRepositoryState `json:"systemPackageRepositories,omitempty"`
	SystemPackages            map[string]*resource.SystemPackageSetState        `json:"systemPackages,omitempty"`
}

// NewSystemState creates a new empty SystemState.
func NewSystemState() *SystemState {
	return &SystemState{
		Version:                   Version,
		SystemInstallers:          make(map[string]*resource.SystemInstallerState),
		SystemPackageRepositories: make(map[string]*resource.SystemPackageRepositoryState),
		SystemPackages:            make(map[string]*resource.SystemPackageSetState),
	}
}
