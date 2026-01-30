package reconciler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/toto/internal/resource"
)

func TestNew(t *testing.T) {
	r := NewToolReconciler()
	assert.NotNil(t, r)
}

func TestReconciler_Reconcile_Install(t *testing.T) {
	// Tool exists in spec but not in state -> Install
	tools := []*resource.Tool{
		{
			BaseResource: resource.BaseResource{
				APIVersion:   "toto.terassyi.net/v1beta1",
				ResourceKind: resource.KindTool,
				Metadata:     resource.Metadata{Name: "ripgrep"},
			},
			ToolSpec: &resource.ToolSpec{
				InstallerRef: "download",
				Version:      "14.1.1",
			},
		},
	}

	states := make(map[string]*resource.ToolState)

	r := NewToolReconciler()
	actions := r.Reconcile(tools, states)

	require.Len(t, actions, 1)

	assert.Equal(t, resource.ActionInstall, actions[0].Type)
	assert.Equal(t, "ripgrep", actions[0].Name)
}

func TestReconciler_Reconcile_Upgrade(t *testing.T) {
	// Tool exists in both but version differs -> Upgrade
	tools := []*resource.Tool{
		{
			BaseResource: resource.BaseResource{
				APIVersion:   "toto.terassyi.net/v1beta1",
				ResourceKind: resource.KindTool,
				Metadata:     resource.Metadata{Name: "ripgrep"},
			},
			ToolSpec: &resource.ToolSpec{
				InstallerRef: "download",
				Version:      "14.2.0", // New version
			},
		},
	}

	states := map[string]*resource.ToolState{
		"ripgrep": {
			InstallerRef: "download",
			Version:      "14.1.1", // Old version
			InstallPath:  "/path/to/rg",
			BinPath:      "/bin/rg",
			UpdatedAt:    time.Now(),
		},
	}

	r := NewToolReconciler()
	actions := r.Reconcile(tools, states)

	require.Len(t, actions, 1)

	assert.Equal(t, resource.ActionUpgrade, actions[0].Type)
	assert.Equal(t, "ripgrep", actions[0].Name)
}

func TestReconciler_Reconcile_Skip(t *testing.T) {
	// Tool exists in both with same version -> Skip (no action)
	tools := []*resource.Tool{
		{
			BaseResource: resource.BaseResource{
				APIVersion:   "toto.terassyi.net/v1beta1",
				ResourceKind: resource.KindTool,
				Metadata:     resource.Metadata{Name: "ripgrep"},
			},
			ToolSpec: &resource.ToolSpec{
				InstallerRef: "download",
				Version:      "14.1.1",
			},
		},
	}

	states := map[string]*resource.ToolState{
		"ripgrep": {
			InstallerRef: "download",
			Version:      "14.1.1", // Same version
			InstallPath:  "/path/to/rg",
			BinPath:      "/bin/rg",
			UpdatedAt:    time.Now(),
		},
	}

	r := NewToolReconciler()
	actions := r.Reconcile(tools, states)

	assert.Empty(t, actions) // No actions needed
}

func TestReconciler_Reconcile_Remove(t *testing.T) {
	// Tool exists in state but not in spec -> Remove
	tools := []*resource.Tool{} // Empty spec

	states := map[string]*resource.ToolState{
		"ripgrep": {
			InstallerRef: "download",
			Version:      "14.1.1",
			InstallPath:  "/path/to/rg",
			BinPath:      "/bin/rg",
			UpdatedAt:    time.Now(),
		},
	}

	r := NewToolReconciler()
	actions := r.Reconcile(tools, states)

	require.Len(t, actions, 1)

	assert.Equal(t, resource.ActionRemove, actions[0].Type)
	assert.Equal(t, "ripgrep", actions[0].Name)
}

func TestReconciler_Reconcile_MultipleTools(t *testing.T) {
	// Multiple tools with different actions
	tools := []*resource.Tool{
		{
			BaseResource: resource.BaseResource{
				APIVersion:   "toto.terassyi.net/v1beta1",
				ResourceKind: resource.KindTool,
				Metadata:     resource.Metadata{Name: "ripgrep"},
			},
			ToolSpec: &resource.ToolSpec{
				InstallerRef: "download",
				Version:      "14.2.0", // Upgrade
			},
		},
		{
			BaseResource: resource.BaseResource{
				APIVersion:   "toto.terassyi.net/v1beta1",
				ResourceKind: resource.KindTool,
				Metadata:     resource.Metadata{Name: "fd"},
			},
			ToolSpec: &resource.ToolSpec{
				InstallerRef: "download",
				Version:      "9.0.0", // New install
			},
		},
	}

	states := map[string]*resource.ToolState{
		"ripgrep": {
			InstallerRef: "download",
			Version:      "14.1.1",
			InstallPath:  "/path/to/rg",
			BinPath:      "/bin/rg",
			UpdatedAt:    time.Now(),
		},
		"bat": {
			InstallerRef: "download",
			Version:      "0.24.0",
			InstallPath:  "/path/to/bat",
			BinPath:      "/bin/bat",
			UpdatedAt:    time.Now(),
		},
	}

	r := NewToolReconciler()
	actions := r.Reconcile(tools, states)

	require.Len(t, actions, 3) // Upgrade ripgrep, Install fd, Remove bat

	// Find actions by name
	actionMap := make(map[string]*Action[*resource.Tool, *resource.ToolState])
	for i := range actions {
		actionMap[actions[i].Name] = &actions[i]
	}

	assert.Equal(t, resource.ActionUpgrade, actionMap["ripgrep"].Type)
	assert.Equal(t, resource.ActionInstall, actionMap["fd"].Type)
	assert.Equal(t, resource.ActionRemove, actionMap["bat"].Type)
}

func TestReconciler_Reconcile_Tainted(t *testing.T) {
	// Tool is tainted -> Reinstall (upgrade action)
	tools := []*resource.Tool{
		{
			BaseResource: resource.BaseResource{
				APIVersion:   "toto.terassyi.net/v1beta1",
				ResourceKind: resource.KindTool,
				Metadata:     resource.Metadata{Name: "ripgrep"},
			},
			ToolSpec: &resource.ToolSpec{
				InstallerRef: "download",
				Version:      "14.1.1",
			},
		},
	}

	states := map[string]*resource.ToolState{
		"ripgrep": {
			InstallerRef: "download",
			Version:      "14.1.1", // Same version
			InstallPath:  "/path/to/rg",
			BinPath:      "/bin/rg",
			TaintReason:  "runtime_upgraded",
			UpdatedAt:    time.Now(),
		},
	}

	r := NewToolReconciler()
	actions := r.Reconcile(tools, states)

	require.Len(t, actions, 1)

	assert.Equal(t, resource.ActionUpgrade, actions[0].Type)
	assert.Equal(t, "ripgrep", actions[0].Name)
	assert.Contains(t, actions[0].Reason, "tainted")
}
