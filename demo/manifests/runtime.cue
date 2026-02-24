package tomei

import gopreset "tomei.terassyi.net/presets/go"

goRuntime: gopreset.#GoRuntime & {
	platform: {os: _os, arch: _arch}
}

goTools: gopreset.#GoToolSet & {
	metadata: {
		name:        "go-tools"
		description: "Go development tools"
	}
	spec: tools: {
		gopls:       {package: "golang.org/x/tools/gopls", version: "latest"}
		staticcheck: {package: "honnef.co/go/tools/cmd/staticcheck", version: "latest"}
		goimports:   {package: "golang.org/x/tools/cmd/goimports", version: "latest"}
	}
}
