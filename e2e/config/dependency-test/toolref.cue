package tomei

// ToolRef dependency test: aqua -> jq -> jq-installer

_jqVersion: "1.7.1"

// jq: uses "macos" instead of "darwin" in URLs
_jqOsMap: {
	linux:  "linux"
	darwin: "macos"
}

_jqSource: {
	url:         "https://github.com/jqlang/jq/releases/download/jq-\(_jqVersion)/jq-\(_jqOsMap[_env.os])-\(_env.arch)"
	archiveType: "raw"
	if _env.os == "linux" && _env.arch == "arm64" {
		checksum: value: "sha256:4dd2d8a0661df0b22f1bb9a1f9830f06b6f3b8f7d91211a1ef5d7c4f06a8b4a5"
	}
	if _env.os == "linux" && _env.arch == "amd64" {
		checksum: value: "sha256:5942c9b0934e510ee61eb3e30273f1b3fe2590df93933a93d7c58b81d19c8ff5"
	}
	if _env.os == "darwin" && _env.arch == "arm64" {
		checksum: value: "sha256:0bbe619e663e0de2c550be2fe0d240d076799d6f8a652b70fa04aea8a8362e8a"
	}
	if _env.os == "darwin" && _env.arch == "amd64" {
		checksum: value: "sha256:4155822bbf5ea90f5c79cf254665975eb4274d426d0709770c21774de5407443"
	}
}

// Base installer (download pattern)
aquaInstaller: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Installer"
	metadata: name: "aqua"
	spec: {
		type: "download"
	}
}

// jq tool installed via aqua
jqTool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "jq"
	spec: {
		installerRef: "aqua"
		version:      _jqVersion
		source:       _jqSource
	}
}

// jq-based installer - depends on jq tool via toolRef
jqInstaller: {
	apiVersion: "tomei.terassyi.net/v1beta1"
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
