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

// TestSchemaVersionCheck_BlocksLoadOnMismatch verifies that
// CheckSchemaVersionForPaths returns an error when schema.cue has a
// different apiVersion than the embedded schema.
func TestSchemaVersionCheck_BlocksLoadOnMismatch(t *testing.T) {
	dir := t.TempDir()

	// Write schema.cue with wrong apiVersion
	wrongSchema := `package tomei

#APIVersion: "tomei.terassyi.net/v0old"
`
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, config.SchemaFileName),
		[]byte(wrongSchema), 0644,
	))

	// Write a valid tool manifest
	toolCue := `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "jq"
spec: {
	installerRef: "aqua"
	version: "1.7.1"
}
`
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "tools.cue"),
		[]byte(toolCue), 0644,
	))

	// CheckSchemaVersionForPaths should detect the mismatch
	err := config.CheckSchemaVersionForPaths([]string{dir})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "apiVersion mismatch")
}

// TestSchemaVersionCheck_PassesOnMatch verifies that
// CheckSchemaVersionForPaths succeeds when schema.cue has the correct
// apiVersion.
func TestSchemaVersionCheck_PassesOnMatch(t *testing.T) {
	dir := t.TempDir()

	// Write schema.cue with correct apiVersion
	correctSchema := `package tomei

#APIVersion: "tomei.terassyi.net/v1beta1"
`
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, config.SchemaFileName),
		[]byte(correctSchema), 0644,
	))

	// Write a valid tool manifest
	toolCue := `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "jq"
spec: {
	installerRef: "aqua"
	version: "1.7.1"
}
`
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "tools.cue"),
		[]byte(toolCue), 0644,
	))

	err := config.CheckSchemaVersionForPaths([]string{dir})
	require.NoError(t, err)
}

// TestSchemaVersionCheck_SkipsWhenNoSchema verifies that
// CheckSchemaVersionForPaths succeeds (skips check) when no schema.cue
// exists in the manifest directory.
func TestSchemaVersionCheck_SkipsWhenNoSchema(t *testing.T) {
	dir := t.TempDir()

	// Only a tool manifest, no schema.cue
	toolCue := `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "jq"
spec: {
	installerRef: "aqua"
	version: "1.7.1"
}
`
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "tools.cue"),
		[]byte(toolCue), 0644,
	))

	err := config.CheckSchemaVersionForPaths([]string{dir})
	require.NoError(t, err)
}
