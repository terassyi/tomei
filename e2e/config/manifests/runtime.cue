package toto

// Go runtime for E2E testing
// Initially installs Go 1.25.6, then can be upgraded to 1.25.7
// Uses _env for OS/arch portability
goRuntime: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "Runtime"
	metadata: name: "go"
	spec: {
		type:    "download"
		version: "1.25.6"
		source: {
			url: "https://go.dev/dl/go\(spec.version).\(_env.os)-\(_env.arch).tar.gz"
			checksum: {
				url: "https://go.dev/dl/?mode=json&include=all"
			}
		}
		binaries: ["go", "gofmt"]
		binDir:      "~/go/bin" // Runtime binaries go to GOBIN (same as toolBinPath)
		toolBinPath: "~/go/bin"
		env: {
			GOROOT: "~/.local/share/toto/runtimes/go/\(spec.version)"
			GOBIN:  "~/go/bin"
		}
		// Commands for tool installation via go install
		commands: {
			install: "go install {{.Package}}@{{.Version}}"
			remove:  "rm -f {{.BinPath}}"
		}
	}
}
