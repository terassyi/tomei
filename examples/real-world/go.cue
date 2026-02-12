package tomei

// Go runtime installed via download from go.dev
goRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Runtime"
	metadata: {
		name:        "go"
		description: "Go programming language runtime"
	}
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
		binDir:      "~/go/bin"
		toolBinPath: "~/go/bin"
		env: {
			GOROOT: "~/.local/share/tomei/runtimes/go/\(spec.version)"
			GOBIN:  "~/go/bin"
		}
		commands: {
			install: "go install {{.Package}}@{{.Version}}"
			remove:  "rm -f {{.BinPath}}"
		}
	}
}

// ToolSet: Go developer tools installed via go install
goTools: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "ToolSet"
	metadata: {
		name:        "go-tools"
		description: "Go development tools installed via go install"
	}
	spec: {
		runtimeRef: "go"
		tools: {
			gopls:       {package: "golang.org/x/tools/gopls", version: "v0.21.0"}
			staticcheck: {package: "honnef.co/go/tools/cmd/staticcheck", version: "v0.6.0"}
			goimports:   {package: "golang.org/x/tools/cmd/goimports", version: "v0.31.0"}
			dlv:         {package: "github.com/go-delve/delve/cmd/dlv", version: "v1.24.2"}
		}
	}
}
