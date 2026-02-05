package manifests

// ripgrep - Install via aqua registry
ripgrep: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "rg"
	spec: {
		installerRef: "aqua"
		version:      "15.1.0"
		package:      "BurntSushi/ripgrep"
	}
}

// fd - Install via aqua registry
fd: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "fd"
	spec: {
		installerRef: "aqua"
		version:      "v10.3.0"
		package:      "sharkdp/fd"
	}
}

// jq - Install via aqua registry
jq: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "jq"
	spec: {
		installerRef: "aqua"
		version:      "jq-1.8.1"
		package:      "jqlang/jq"
	}
}
