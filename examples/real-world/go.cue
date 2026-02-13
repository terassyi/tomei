package tomei

import gopreset "tomei.terassyi.net/presets/go"

_os:   string @tag(os)
_arch: string @tag(arch)

// Go runtime installed via download from go.dev
goRuntime: gopreset.#GoRuntime & {
	platform: {os: _os, arch: _arch}
	spec: version: "1.25.6"
}

// ToolSet: Go developer tools installed via go install
goTools: gopreset.#GoToolSet & {
	metadata: name: "go-tools"
	spec: tools: {
		gopls:       {package: "golang.org/x/tools/gopls", version: "v0.21.0"}
		staticcheck: {package: "honnef.co/go/tools/cmd/staticcheck", version: "v0.6.0"}
		goimports:   {package: "golang.org/x/tools/cmd/goimports", version: "v0.31.0"}
		dlv:         {package: "github.com/go-delve/delve/cmd/dlv", version: "v1.24.2"}
	}
}
