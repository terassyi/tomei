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

// setupMinimalCueMod creates a minimal cue.mod/module.cue in dir for tests.
func setupMinimalCueMod(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cue.mod"), 0755))
	moduleCue := "module: \"test.local@v0\"\nlanguage: version: \"v0.9.0\"\n"
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "cue.mod", "module.cue"),
		[]byte(moduleCue), 0644,
	))
}

// TestSchemaValidation_InstallerDependsOn tests CUE schema validation for dependsOn field.
func TestSchemaValidation_InstallerDependsOn(t *testing.T) {
	tests := []struct {
		name    string
		cue     string
		wantErr bool
	}{
		{
			name: "delegation with dependsOn",
			cue: `package tomei

inst: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "krew-installer"
	spec: {
		type: "delegation"
		toolRef: "krew"
		dependsOn: ["kubectl"]
		commands: install: ["krew install {{.Package}}"]
	}
}
`,
			wantErr: false,
		},
		{
			name: "download with dependsOn",
			cue: `package tomei

inst: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "custom-installer"
	spec: {
		type: "download"
		dependsOn: ["kubectl"]
	}
}
`,
			wantErr: false,
		},
		{
			name: "dependsOn single item",
			cue: `package tomei

inst: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "single-dep"
	spec: {
		type: "download"
		dependsOn: ["single-item"]
	}
}
`,
			wantErr: false,
		},
		{
			name: "dependsOn invalid type",
			cue: `package tomei

inst: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "bad-type"
	spec: {
		type: "download"
		dependsOn: 123
	}
}
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			setupMinimalCueMod(t, dir)
			require.NoError(t, os.WriteFile(
				filepath.Join(dir, "installer.cue"),
				[]byte(tt.cue), 0644,
			))

			loader := config.NewLoader(nil)
			_, err := loader.Load(dir)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestSchemaImport_WorksWithoutImport verifies that loading without
// schema import still validates resources via the internal schema.
func TestSchemaImport_WorksWithoutImport(t *testing.T) {
	dir := t.TempDir()
	setupMinimalCueMod(t, dir)

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
