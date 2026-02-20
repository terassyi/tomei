package tomei

// Mock runtime with alias version for testing --update-runtimes.
// Uses a delegation pattern with ResolveVersion that echos a version string.
// The resolved version can be changed by updating the echo output.

_runtimeVersion: "stable"

mockRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Runtime"
	metadata: name: "mock-rt"
	spec: {
		type:    "delegation"
		version: _runtimeVersion
		bootstrap: {
			install: ["mkdir -p /tmp/mock-rt && echo installed > /tmp/mock-rt/marker"]
			update: ["echo updated > /tmp/mock-rt/marker"]
			check: ["test -d /tmp/mock-rt"]
			remove: ["rm -rf /tmp/mock-rt"]
			resolveVersion: ["echo 1.0.0"]
		}
		binaries: []
		toolBinPath: "/tmp/mock-rt/bin"
		commands: {
			install: ["mkdir -p /tmp/mock-rt/tools && echo 'tool {{.Package}}@{{.Version}} installed'"]
			remove: ["echo 'tool {{.Name}} removed'"]
		}
	}
}
