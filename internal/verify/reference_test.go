package verify

import (
	"testing"

	"cuelang.org/go/mod/module"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReferenceResolver_Resolve(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cueRegistry string
		dep         module.Version
		want        string
		wantErr     bool
	}{
		{
			name:        "default registry mapping",
			cueRegistry: "tomei.terassyi.net=ghcr.io/terassyi",
			dep:         module.MustNewVersion("tomei.terassyi.net@v0", "v0.0.3"),
			want:        "ghcr.io/terassyi/tomei.terassyi.net:v0.0.3",
		},
		{
			name:        "submodule path",
			cueRegistry: "tomei.terassyi.net=ghcr.io/terassyi",
			dep:         module.MustNewVersion("tomei.terassyi.net/presets/go@v0", "v0.0.1"),
			want:        "ghcr.io/terassyi/tomei.terassyi.net/presets/go:v0.0.1",
		},
		{
			name:        "none registry",
			cueRegistry: "none",
			dep:         module.MustNewVersion("tomei.terassyi.net@v0", "v0.0.3"),
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
			assert.Equal(t, tt.want, got.String())
		})
	}
}
