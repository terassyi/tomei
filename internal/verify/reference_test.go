package verify

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReferenceResolver_Resolve(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cueRegistry string
		dep         ModuleDependency
		want        string
		wantErr     bool
	}{
		{
			name:        "default registry mapping",
			cueRegistry: "tomei.terassyi.net=ghcr.io/terassyi",
			dep:         ModuleDependency{ModulePath: "tomei.terassyi.net@v0", Version: "v0.0.3"},
			want:        "ghcr.io/terassyi/tomei.terassyi.net:v0.0.3",
		},
		{
			name:        "submodule path",
			cueRegistry: "tomei.terassyi.net=ghcr.io/terassyi",
			dep:         ModuleDependency{ModulePath: "tomei.terassyi.net/presets/go@v0", Version: "v0.0.1"},
			want:        "ghcr.io/terassyi/tomei.terassyi.net/presets/go:v0.0.1",
		},
		{
			name:        "none registry",
			cueRegistry: "none",
			dep:         ModuleDependency{ModulePath: "tomei.terassyi.net@v0", Version: "v0.0.3"},
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resolver, err := NewReferenceResolver(tt.cueRegistry)
			require.NoError(t, err)

			got, err := resolver.Resolve(tt.dep)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSplitModulePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		modulePath string
		wantBase   string
	}{
		{
			name:       "with major version",
			modulePath: "tomei.terassyi.net@v0",
			wantBase:   "tomei.terassyi.net",
		},
		{
			name:       "submodule with major version",
			modulePath: "tomei.terassyi.net/presets/go@v0",
			wantBase:   "tomei.terassyi.net/presets/go",
		},
		{
			name:       "no major version",
			modulePath: "tomei.terassyi.net",
			wantBase:   "tomei.terassyi.net",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := splitModulePath(tt.modulePath)
			assert.Equal(t, tt.wantBase, got)
		})
	}
}
