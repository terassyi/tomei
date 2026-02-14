package manifests

_rgVersion: "15.1.0"
_fdVersion: "v10.3.0"
_jqVersion: "1.8.1"

// ripgrep - Install via aqua registry
ripgrep: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "rg"
	spec: {
		installerRef: "aqua"
		version:      _rgVersion
		package:      "BurntSushi/ripgrep"
	}
}

// fd - Install via aqua registry
fd: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "fd"
	spec: {
		installerRef: "aqua"
		version:      _fdVersion
		package:      "sharkdp/fd"
	}
}

// jq - Install via aqua registry
jq: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "jq"
	spec: {
		installerRef: "aqua"
		version:      _jqVersion
		package:      "jqlang/jq"
	}
}
