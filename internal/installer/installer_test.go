package installer

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/toto/internal/resource"
)

// mockInstaller is a mock implementation of Installer for testing.
type mockInstaller[S any, T any] struct {
	installFunc func(ctx context.Context, spec S, name string) (T, error)
}

func (m *mockInstaller[S, T]) Install(ctx context.Context, spec S, name string) (T, error) {
	if m.installFunc != nil {
		return m.installFunc(ctx, spec, name)
	}
	var zero T
	return zero, nil
}

func TestInstaller_Interface(t *testing.T) {
	// Verify mockInstaller implements Installer interface
	var _ Installer = (*mockInstaller[*resource.ToolSpec, *resource.ToolState])(nil)

	tests := []struct {
		name        string
		spec        *resource.ToolSpec
		toolName    string
		installFunc func(ctx context.Context, spec *resource.ToolSpec, name string) (*resource.ToolState, error)
		wantState   *resource.ToolState
		wantErr     bool
	}{
		{
			name: "successful install",
			spec: &resource.ToolSpec{
				InstallerRef: "download",
				Version:      "1.0.0",
			},
			toolName: "test-tool",
			installFunc: func(ctx context.Context, spec *resource.ToolSpec, name string) (*resource.ToolState, error) {
				return &resource.ToolState{
					InstallerRef: spec.InstallerRef,
					Version:      spec.Version,
					InstallPath:  "/path/to/" + name,
					BinPath:      "/bin/" + name,
				}, nil
			},
			wantState: &resource.ToolState{
				InstallerRef: "download",
				Version:      "1.0.0",
				InstallPath:  "/path/to/test-tool",
				BinPath:      "/bin/test-tool",
			},
			wantErr: false,
		},
		{
			name: "install with runtime ref",
			spec: &resource.ToolSpec{
				InstallerRef: "go",
				Version:      "0.16.0",
				RuntimeRef:   "go",
				Package:      "golang.org/x/tools/gopls",
			},
			toolName: "gopls",
			installFunc: func(ctx context.Context, spec *resource.ToolSpec, name string) (*resource.ToolState, error) {
				return &resource.ToolState{
					InstallerRef: spec.InstallerRef,
					Version:      spec.Version,
					RuntimeRef:   spec.RuntimeRef,
					Package:      spec.Package,
					InstallPath:  "/go/bin/" + name,
					BinPath:      "/bin/" + name,
				}, nil
			},
			wantState: &resource.ToolState{
				InstallerRef: "go",
				Version:      "0.16.0",
				RuntimeRef:   "go",
				Package:      "golang.org/x/tools/gopls",
				InstallPath:  "/go/bin/gopls",
				BinPath:      "/bin/gopls",
			},
			wantErr: false,
		},
		{
			name: "install error",
			spec: &resource.ToolSpec{
				InstallerRef: "download",
				Version:      "1.0.0",
			},
			toolName: "failing-tool",
			installFunc: func(ctx context.Context, spec *resource.ToolSpec, name string) (*resource.ToolState, error) {
				return nil, errors.New("download failed")
			},
			wantState: nil,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockInstaller[*resource.ToolSpec, *resource.ToolState]{
				installFunc: tt.installFunc,
			}

			state, err := mock.Install(context.Background(), tt.spec, tt.toolName)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantState, state)
		})
	}
}
