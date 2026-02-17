package cuemod

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScaffold(t *testing.T) {
	kinds := SupportedScaffoldKinds()
	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			out, err := Scaffold(kind, ScaffoldParams{Bare: false})
			require.NoError(t, err)
			s := string(out)
			assert.Contains(t, s, "package tomei")
			assert.Contains(t, s, `import "tomei.terassyi.net/schema"`)
			assert.Contains(t, s, `apiVersion: "tomei.terassyi.net/v1beta1"`)
		})
	}
}

func TestScaffold_Bare(t *testing.T) {
	kinds := SupportedScaffoldKinds()
	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			out, err := Scaffold(kind, ScaffoldParams{Bare: true})
			require.NoError(t, err)
			s := string(out)
			assert.Contains(t, s, "package tomei")
			assert.NotContains(t, s, "import")
			assert.NotContains(t, s, "schema.#")
			assert.Contains(t, s, `apiVersion: "tomei.terassyi.net/v1beta1"`)
		})
	}
}

func TestScaffold_UnknownKind(t *testing.T) {
	_, err := Scaffold("unknown", ScaffoldParams{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported kind")
}

func TestScaffold_KindSpecificContent(t *testing.T) {
	tests := []struct {
		kind     string
		contains string
	}{
		{"tool", `kind:       "Tool"`},
		{"runtime", `kind:       "Runtime"`},
		{"installer", `kind:       "Installer"`},
		{"installer-repository", `kind:       "InstallerRepository"`},
		{"toolset", `kind:       "ToolSet"`},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			out, err := Scaffold(tt.kind, ScaffoldParams{Bare: false})
			require.NoError(t, err)
			assert.Contains(t, string(out), tt.contains)
		})
	}
}
