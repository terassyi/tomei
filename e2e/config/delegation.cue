package toto

// gopls - Go language server installed via go install (Runtime Delegation)
gopls: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "gopls"
	spec: {
		runtimeRef: "go"
		package:    "golang.org/x/tools/gopls"
		version:    "v0.21.0"
	}
}
