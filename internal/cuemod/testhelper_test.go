package cuemod

import (
	"maps"
	"testing/fstest"
)

// buildMockModuleFS creates a minimal CUE module FS for the mock registry.
// Optional extra files are merged into the FS under the version prefix.
func buildMockModuleFS(version string, extra ...fstest.MapFS) fstest.MapFS {
	prefix := "tomei.terassyi.net_" + version + "/"
	fs := fstest.MapFS{
		prefix + "cue.mod/module.cue": &fstest.MapFile{
			Data: []byte("module: \"tomei.terassyi.net@v0\"\nlanguage: version: \"v0.9.0\"\n"),
		},
		prefix + "schema/schema.cue": &fstest.MapFile{
			Data: []byte("package schema\n"),
		},
	}
	for _, e := range extra {
		for k, v := range e {
			fs[prefix+k] = v
		}
	}
	return fs
}

// mergeMockModuleFS merges multiple version FSes into one.
func mergeMockModuleFS(versions ...string) fstest.MapFS {
	merged := fstest.MapFS{}
	for _, v := range versions {
		maps.Copy(merged, buildMockModuleFS(v))
	}
	return merged
}
