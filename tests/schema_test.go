//go:build integration

package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/tomei/internal/config"
)

// TestSchemaImport_LoadWithImport verifies that manifests using
// import "tomei.terassyi.net/schema" are loaded successfully.
func TestSchemaImport_LoadWithImport(t *testing.T) {
	dir := t.TempDir()

	toolCue := `package tomei

import "tomei.terassyi.net/schema"

myTool: schema.#Tool & {
	metadata: name: "jq"
	spec: {
		installerRef: "aqua"
		version:      "1.7.1"
		package:      "jqlang/jq"
	}
}
`
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "tools.cue"),
		[]byte(toolCue), 0644,
	))

	loader := config.NewLoader(nil)
	resources, err := loader.Load(dir)
	require.NoError(t, err)
	require.Len(t, resources, 1)
	assert.Equal(t, "jq", resources[0].Name())
}

// TestSchemaImport_WorksWithoutImport verifies that loading without
// schema import still validates resources via the internal schema.
func TestSchemaImport_WorksWithoutImport(t *testing.T) {
	dir := t.TempDir()

	toolCue := `package tomei

myTool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "jq"
	spec: {
		installerRef: "aqua"
		version:      "1.7.1"
		package:      "jqlang/jq"
	}
}
`
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "tools.cue"),
		[]byte(toolCue), 0644,
	))

	loader := config.NewLoader(nil)
	resources, err := loader.Load(dir)
	require.NoError(t, err)
	require.Len(t, resources, 1)
	assert.Equal(t, "jq", resources[0].Name())
}
