package config

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/mod/modconfig"
	"cuelang.org/go/mod/modregistrytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTagImportBehavior verifies that CUE's @tag() injection does NOT propagate
// to imported packages resolved via a registry. This is a confirmed behavior in
// CUE v0.15+ and determines our env injection strategy:
//   - Tags work only in the top-level instance (user manifest).
//   - Imported packages cannot use @tag() to receive values from the caller.
//   - Therefore, presets that need platform info accept explicit parameters.
func TestTagImportBehavior(t *testing.T) {
	// Build an in-memory registry module "example.com@v0" with a package
	// "mypkg" that declares `_os: string @tag(os)`.
	registryFS := fstest.MapFS{
		"example.com_v0.0.1/cue.mod/module.cue": &fstest.MapFile{
			Data: []byte(`module: "example.com@v0"
language: version: "v0.9.0"
`),
		},
		"example.com_v0.0.1/mypkg/mypkg.cue": &fstest.MapFile{
			Data: []byte(`package mypkg

_os: string @tag(os)

#MyDef: {
    url: "https://example.com/\(_os)/file.tar.gz"
}
`),
		},
	}

	reg, err := modregistrytest.New(registryFS, "")
	require.NoError(t, err)
	defer reg.Close()

	// Create a temporary directory with a user manifest that imports mypkg.
	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cue.mod"), 0755))
	moduleCUE := `module: "my.manifest@v0"
language: version: "v0.9.0"
deps: {
    "example.com@v0": v: "v0.0.1"
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cue.mod", "module.cue"), []byte(moduleCUE), 0644))

	// The user manifest declares @tag(os) at top level AND imports the
	// registry package which also declares @tag(os).
	manifestCUE := `package tomei

import "example.com/mypkg"

_os: string @tag(os)

result: mypkg.#MyDef
localURL: "https://example.com/\(_os)/local.tar.gz"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.cue"), []byte(manifestCUE), 0644))

	// Configure the CUE loader with registry and tags
	registry, err := modconfig.NewRegistry(&modconfig.Config{
		Env: []string{
			"CUE_REGISTRY=" + reg.Host() + "+insecure",
		},
	})
	require.NoError(t, err)

	instances := load.Instances([]string{"main.cue"}, &load.Config{
		Dir:      dir,
		Registry: registry,
		Tags:     []string{"os=linux"},
	})
	require.Len(t, instances, 1)
	require.NoError(t, instances[0].Err, "CUE load should succeed")

	ctx := cuecontext.New()
	value := ctx.BuildInstance(instances[0])
	require.NoError(t, value.Err(), "CUE build should succeed")

	// Top-level @tag(os) should resolve correctly
	localURL := value.LookupPath(cue.ParsePath("localURL"))
	require.True(t, localURL.Exists(), "localURL should exist")
	localStr, err := localURL.String()
	require.NoError(t, err, "localURL should be concrete")
	assert.Equal(t, "https://example.com/linux/local.tar.gz", localStr,
		"@tag(os) should work for top-level instance")

	// Imported package's @tag(os) should NOT be resolved — the URL will be
	// non-concrete because _os in the imported package remains unresolved.
	resultURL := value.LookupPath(cue.ParsePath("result.url"))
	require.True(t, resultURL.Exists(), "result.url should exist")

	_, err = resultURL.String()
	assert.Error(t, err, "@tag(os) should NOT propagate to imported packages via registry; result.url should be non-concrete")
}
