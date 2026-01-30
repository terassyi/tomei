package installer

import (
	"context"

	"github.com/terassyi/toto/internal/resource"
)

// Installer defines the interface for tool installation.
type Installer interface {
	// Install installs a tool according to the spec and returns its state.
	Install(ctx context.Context, spec *resource.ToolSpec, name string) (*resource.ToolState, error)
}
