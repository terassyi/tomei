package toto

// Runtime to Tool dependency chain test: go runtime -> go installer -> gopls

_goVersion: "1.23.5"

_goSource: {
	url: "https://go.dev/dl/go\(_goVersion).\(_env.os)-\(_env.arch).tar.gz"
	checksum: url: "https://go.dev/dl/?mode=json&include=all"
}

// Go Runtime
goRuntime: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "Runtime"
	metadata: name: "go"
	spec: {
		type:    "download"
		version: _goVersion
		source:  _goSource
		binaries: ["go", "gofmt"]
		binDir:      "~/go/bin"
		toolBinPath: "~/go/bin"
		env: {
			GOROOT: "~/.local/share/toto/runtimes/go/\(_goVersion)"
			GOBIN:  "~/go/bin"
		}
		commands: {
			install: "go install {{.Package}}@{{.Version}}"
			remove:  "rm -f {{.BinPath}}"
		}
	}
}

// Go Installer - depends on Go Runtime via runtimeRef
goInstaller: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "Installer"
	metadata: name: "go"
	spec: {
		type:       "delegation"
		runtimeRef: "go"
		commands: {
			install: "go install {{.Package}}{{if .Version}}@{{.Version}}{{end}}"
		}
	}
}

// gopls Tool - installed via go installer
gopls: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "gopls"
	spec: {
		installerRef: "go"
		runtimeRef:   "go"
		package:      "golang.org/x/tools/gopls"
		version:      "v0.17.1"
	}
}
