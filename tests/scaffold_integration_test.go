//go:build integration

package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/tomei/internal/config"
	"github.com/terassyi/tomei/internal/cuemod"
	"github.com/terassyi/tomei/internal/resource"
)

// writePlatformCue writes the tomei_platform.cue file for @tag() support.
func writePlatformCue(t *testing.T, dir string) {
	t.Helper()
	platformCue, err := cuemod.GeneratePlatformCUE()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tomei_platform.cue"), platformCue, 0644))
}

func TestScaffold_OutputIsLoadable(t *testing.T) {
	tests := []struct {
		kind     string
		wantKind resource.Kind
	}{
		{"tool", resource.KindTool},
		{"runtime", resource.KindRuntime},
		{"installer", resource.KindInstaller},
		{"installer-repository", resource.KindInstallerRepository},
		{"toolset", resource.KindToolSet},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			// Generate bare scaffold output (no schema import, works without registry)
			out, err := cuemod.Scaffold(tt.kind, cuemod.ScaffoldParams{Bare: true})
			require.NoError(t, err)

			dir := t.TempDir()
			setupMinimalCueMod(t, dir)
			// Runtime template uses _os, _arch so platform.cue is needed
			writePlatformCue(t, dir)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "scaffold.cue"), out, 0644))

			loader := config.NewLoader(&config.Env{OS: "linux", Arch: "amd64"})
			resources, err := loader.Load(dir)
			require.NoError(t, err)
			require.Len(t, resources, 1)
			assert.Equal(t, tt.wantKind, resources[0].Kind())
		})
	}
}
