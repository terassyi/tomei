package tomei

ripgrep: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "rg"
	spec: {
		installerRef: "aqua"
		version:      "15.1.0"
		package:      "BurntSushi/ripgrep"
	}
}

fd: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "fd"
	spec: {
		installerRef: "aqua"
		version:      "v10.3.0"
		package:      "sharkdp/fd"
	}
}

jq: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "jq"
	spec: {
		installerRef: "aqua"
		version:      "jq-1.8.1"
		package:      "jqlang/jq"
	}
}

bat: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "bat"
	spec: {
		installerRef: "aqua"
		version:      "v0.26.1"
		package:      "sharkdp/bat"
	}
}
