package tomei

_os:   string @tag(os)
_arch: string @tag(arch)

goRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Runtime"
	metadata: name: "go"
	spec: {
		type:    "download"
		version: "1.25.6"
		source: {
			url: "https://go.dev/dl/go\(spec.version).\(_os)-\(_arch).tar.gz"
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

gopls: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "gopls"
	spec: {
		runtimeRef: "go"
		package:    "golang.org/x/tools/gopls"
		version:    "v0.21.0"
	}
}

staticcheck: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "staticcheck"
	spec: {
		runtimeRef: "go"
		package:    "honnef.co/go/tools/cmd/staticcheck"
		version:    "v0.6.0"
	}
}

goimports: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "goimports"
	spec: {
		runtimeRef: "go"
		package:    "golang.org/x/tools/cmd/goimports"
		version:    "v0.31.0"
	}
}
