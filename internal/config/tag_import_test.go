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

// TestTagImportBehavior verifies whether CUE v0.15's @tag() injection
// propagates to imported packages resolved via a registry.
// This determines the env injection strategy for Phase G:
//   - If tags propagate: presets can use @tag() and no env overlay is needed.
//   - If tags do NOT propagate: embedded registry must inject concrete env values.
func TestTagImportBehavior(t *testing.T) {
	// Build an in-memory registry module "example.com@v0" with a package
	// "mypkg" that declares `_os: string @tag(os)`.
	//
	// modregistrytest.New expects directories named path_vers where
	// slashes in the module path are replaced with underscores.
	// For "example.com@v0" version "v0.0.1", dir = "example.com_v0.0.1"
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

	// Write cue.mod/module.cue declaring a dependency on example.com@v0
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
	// If tags propagate to imports, the imported _os will be concrete.
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

	inst := instances[0]
	if inst.Err != nil {
		t.Logf("Load error: %v", inst.Err)
		t.Log("RESULT: @tag() does NOT propagate to imported packages via registry")
		t.Log("Strategy: embedded registry must inject concrete _os/_arch values")
		return
	}

	ctx := cuecontext.New()
	value := ctx.BuildInstance(inst)
	if value.Err() != nil {
		t.Logf("Build error: %v", value.Err())
		t.Log("RESULT: @tag() does NOT propagate to imported packages via registry")
		t.Log("Strategy: embedded registry must inject concrete _os/_arch values")
		return
	}

	// Verify local @tag(os) works in the top-level instance
	localURL := value.LookupPath(cue.ParsePath("localURL"))
	require.True(t, localURL.Exists(), "localURL should exist")
	localStr, err := localURL.String()
	require.NoError(t, err, "localURL should be concrete")
	assert.Equal(t, "https://example.com/linux/local.tar.gz", localStr,
		"@tag(os) should work for top-level instance")

	// Now check if @tag(os) propagated to the imported package
	resultURL := value.LookupPath(cue.ParsePath("result.url"))
	require.True(t, resultURL.Exists(), "result.url should exist")

	urlStr, err := resultURL.String()
	if err != nil {
		t.Logf("Cannot get concrete string for result.url: %v", err)
		t.Log("RESULT: @tag() does NOT propagate to imported packages via registry")
		t.Log("Strategy: embedded registry must inject concrete _os/_arch values")
		return
	}

	if urlStr == "https://example.com/linux/file.tar.gz" {
		t.Log("RESULT: @tag() DOES propagate to imported packages via registry!")
		t.Log("Strategy: presets can use @tag() directly, no env overlay needed for registry path")
		assert.Equal(t, "https://example.com/linux/file.tar.gz", urlStr)
	} else {
		t.Logf("URL resolved to unexpected value: %s", urlStr)
		t.Log("RESULT: @tag() does NOT propagate correctly")
	}
}
