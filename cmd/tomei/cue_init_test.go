package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestCueInitCmd creates an isolated cue init command for testing.
func newTestCueInitCmd() *cobra.Command {
	var moduleName string
	var force bool

	cmd := &cobra.Command{
		Use:  "init [dir]",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Save/restore package vars
			origModule, origForce := cueInitModuleName, cueInitForce
			defer func() { cueInitModuleName, cueInitForce = origModule, origForce }()
			cueInitModuleName = moduleName
			cueInitForce = force
			return runCueInit(cmd, args)
		},
	}
	cmd.Flags().StringVar(&moduleName, "module-name", defaultModuleName, "")
	cmd.Flags().BoolVar(&force, "force", false, "")
	cmd.SetOut(&bytes.Buffer{})
	return cmd
}

func TestCueInit_CreatesFiles(t *testing.T) {
	dir := t.TempDir()

	cmd := newTestCueInitCmd()
	cmd.SetArgs([]string{dir})
	require.NoError(t, cmd.Execute())

	// Verify cue.mod/module.cue was created
	moduleCue, err := os.ReadFile(filepath.Join(dir, "cue.mod", "module.cue"))
	require.NoError(t, err)
	assert.Contains(t, string(moduleCue), `module: "manifests.local@v0"`)
	assert.Contains(t, string(moduleCue), `language: version: "v0.9.0"`)
	assert.Contains(t, string(moduleCue), `"tomei.terassyi.net@v0"`)

	// Verify tomei_platform.cue was created
	platformCue, err := os.ReadFile(filepath.Join(dir, "tomei_platform.cue"))
	require.NoError(t, err)
	assert.Contains(t, string(platformCue), `package tomei`)
	assert.Contains(t, string(platformCue), `_os:       string @tag(os)`)
	assert.Contains(t, string(platformCue), `_arch:     string @tag(arch)`)
	assert.Contains(t, string(platformCue), `_headless: bool | *false @tag(headless,type=bool)`)
}

func TestCueInit_CustomModuleName(t *testing.T) {
	dir := t.TempDir()

	cmd := newTestCueInitCmd()
	cmd.SetArgs([]string{"--module-name", "myproject@v0", dir})
	require.NoError(t, cmd.Execute())

	moduleCue, err := os.ReadFile(filepath.Join(dir, "cue.mod", "module.cue"))
	require.NoError(t, err)
	assert.Contains(t, string(moduleCue), `module: "myproject@v0"`)
}

func TestCueInit_RefusesOverwrite(t *testing.T) {
	dir := t.TempDir()

	// Create existing file
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cue.mod"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cue.mod", "module.cue"), []byte("existing"), 0644))

	cmd := newTestCueInitCmd()
	cmd.SetArgs([]string{dir})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")

	// Verify existing file is unchanged
	data, err := os.ReadFile(filepath.Join(dir, "cue.mod", "module.cue"))
	require.NoError(t, err)
	assert.Equal(t, "existing", string(data))
}

func TestCueInit_ForceOverwrites(t *testing.T) {
	dir := t.TempDir()

	// Create existing file
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cue.mod"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cue.mod", "module.cue"), []byte("existing"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tomei_platform.cue"), []byte("existing"), 0644))

	cmd := newTestCueInitCmd()
	cmd.SetArgs([]string{"--force", dir})
	require.NoError(t, cmd.Execute())

	// Verify file was overwritten
	data, err := os.ReadFile(filepath.Join(dir, "cue.mod", "module.cue"))
	require.NoError(t, err)
	assert.Contains(t, string(data), `module: "manifests.local@v0"`)
}

func TestCueInit_CurrentDir(t *testing.T) {
	dir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origDir) }()
	require.NoError(t, os.Chdir(dir))

	cmd := newTestCueInitCmd()
	cmd.SetArgs([]string{})
	require.NoError(t, cmd.Execute())

	// Verify files were created in current directory
	_, err = os.Stat(filepath.Join(dir, "cue.mod", "module.cue"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, "tomei_platform.cue"))
	require.NoError(t, err)
}

func TestCueInit_CreatesTargetDir(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "subdir", "manifests")

	cmd := newTestCueInitCmd()
	cmd.SetArgs([]string{dir})
	require.NoError(t, cmd.Execute())

	_, err := os.Stat(filepath.Join(dir, "cue.mod", "module.cue"))
	assert.NoError(t, err)
}

func TestGenerateModuleCUE(t *testing.T) {
	content := string(generateModuleCUE("test@v0"))
	assert.Contains(t, content, `module: "test@v0"`)
	assert.Contains(t, content, `language: version: "v0.9.0"`)
	assert.Contains(t, content, `"tomei.terassyi.net@v0": v: "v0.0.1"`)
}

func TestGeneratePlatformCUE(t *testing.T) {
	content := string(generatePlatformCUE())
	assert.Contains(t, content, `package tomei`)
	assert.Contains(t, content, `@tag(os)`)
	assert.Contains(t, content, `@tag(arch)`)
	assert.Contains(t, content, `@tag(headless,type=bool)`)
}
