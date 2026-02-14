package tomei

import gopreset "tomei.terassyi.net/presets/go"

// Go developer tools installed via go install
goTools: gopreset.#GoToolSet & {
	metadata: {
		name:        "go-tools"
		description: "Go development tools"
	}
	spec: tools: {
		gopls: {package: "golang.org/x/tools/gopls", version: "v0.21.1"}
		staticcheck: {package: "honnef.co/go/tools/cmd/staticcheck", version: "v0.7.0"}
		goimports: {package: "golang.org/x/tools/cmd/goimports", version: "v0.42.0"}
		cue: {package: "cuelang.org/go/cmd/cue", version: "v0.15.4"}
	}
}
