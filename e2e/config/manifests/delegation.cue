package tomei

// gopls - Go language server installed via go install (Runtime Delegation)

_goplsVersion: "v0.21.0"

gopls: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "gopls"
	spec: {
		runtimeRef: "go"
		package:    "golang.org/x/tools/gopls"
		version:    _goplsVersion
	}
}

// goimports - Go import organizer installed via go install (Runtime Delegation)
// Tests that multiple delegation tools for the same runtime are installed correctly.

_goimportsVersion: "v0.33.0"

goimports: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "goimports"
	spec: {
		runtimeRef: "go"
		package:    "golang.org/x/tools/cmd/goimports"
		version:    _goimportsVersion
	}
}
