package tomei

// Circular dependency test: Installer(a) -> Tool(b) -> Installer(a)
installerA: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Installer"
	metadata: name: "installer-a"
	spec: {
		type:    "delegation"
		toolRef: "tool-b"
		commands: {
			install: ["installer-a install {{.Package}}"]
		}
	}
}

toolB: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "tool-b"
	spec: {
		installerRef: "installer-a"
		version:      "1.0.0"
	}
}
