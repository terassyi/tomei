package toto

// ToolRef dependency test: aqua -> jq -> jq-installer

// Base installer (download pattern)
aquaInstaller: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "Installer"
	metadata: name: "aqua"
	spec: {
		type: "download"
	}
}

// jq tool installed via aqua registry
jqTool: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "jq"
	spec: {
		installerRef: "aqua"
		version:      "1.7.1"
		package: {
			registry: "aqua"
			name:     "jqlang/jq"
		}
	}
}

// jq-based installer - depends on jq tool via toolRef
jqInstaller: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "Installer"
	metadata: name: "jq-installer"
	spec: {
		type:    "delegation"
		toolRef: "jq"
		commands: {
			install: "jq --version && echo 'Installing {{.Package}} via jq-installer'"
		}
	}
}
