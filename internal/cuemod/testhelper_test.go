package cuemod

import (
	"maps"
	"testing/fstest"
)

// buildMockModuleFS creates a minimal CUE module FS for the mock registry.
func buildMockModuleFS(version string) fstest.MapFS {
	prefix := "tomei.terassyi.net_" + version + "/"
	return fstest.MapFS{
		prefix + "cue.mod/module.cue": &fstest.MapFile{
			Data: []byte("module: \"tomei.terassyi.net@v0\"\nlanguage: version: \"v0.9.0\"\n"),
		},
		prefix + "schema/schema.cue": &fstest.MapFile{
			Data: []byte("package schema\n"),
		},
	}
}

// mergeMockModuleFS merges multiple version FSes into one.
func mergeMockModuleFS(versions ...string) fstest.MapFS {
	merged := fstest.MapFS{}
	for _, v := range versions {
		maps.Copy(merged, buildMockModuleFS(v))
	}
	return merged
}
