package tomei

// ToolSet: multiple Go tools installed via runtime delegation
goTools: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "ToolSet"
	metadata: name: "go-tools"
	spec: {
		runtimeRef: "go"
		tools: {
			staticcheck: {package: "honnef.co/go/tools/cmd/staticcheck", version: "v0.6.0"}
			godoc: {package: "golang.org/x/tools/cmd/godoc", version: "v0.31.0"}
		}
	}
}
