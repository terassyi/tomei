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

// jq tool installed via aqua
jqTool: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "jq"
	spec: {
		installerRef: "aqua"
		version:      "1.7.1"
		source: {
			url: "https://github.com/jqlang/jq/releases/download/jq-1.7.1/jq-linux-arm64"
			checksum: {
				value: "sha256:4dd2d8a0661df0b22f1bb9a1f9830f06b6f3b8f7d91211a1ef5d7c4f06a8b4a5"
			}
			archiveType: "raw"
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
