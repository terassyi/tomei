package toto

// Go runtime for E2E testing
goRuntime: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "Runtime"
	metadata: name: "go"
	spec: {
		installerRef: "download"
		version:      "1.25.5"
		source: {
			url: "https://go.dev/dl/go1.25.5.linux-amd64.tar.gz"
			checksum: {
				value: "sha256:9e9b755d63b36acf30c12a9a3fc379243714c1c6d3dd72861da637f336ebb35b"
			}
		}
		binaries: ["go", "gofmt"]
		toolBinPath: "~/go/bin"
		env: {
			GOROOT: "~/.local/share/toto/runtimes/go/1.25.5"
			GOBIN:  "~/go/bin"
		}
		// Commands for tool installation via go install
		commands: {
			install: "go install {{.Package}}@{{.Version}}"
			remove:  "rm -f {{.BinPath}}"
		}
	}
}
