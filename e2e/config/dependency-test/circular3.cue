package tomei

// 3-node circular dependency test: A -> B -> C -> A
toolA: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "tool-a"
	spec: {
		installerRef: "installer-c"
		version:      "1.0.0"
	}
}

installerB: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Installer"
	metadata: name: "installer-b"
	spec: {
		type:    "delegation"
		toolRef: "tool-a"
		commands: {install: ["echo install"]}
	}
}

toolC: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "tool-c"
	spec: {
		installerRef: "installer-b"
		version:      "1.0.0"
	}
}

installerC: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Installer"
	metadata: name: "installer-c"
	spec: {
		type:    "delegation"
		toolRef: "tool-c"
		commands: {install: ["echo install"]}
	}
}
