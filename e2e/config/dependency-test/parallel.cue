package toto

// Parallel tool installation test: multiple independent tools

_rgSource: {
	if _env.os == "linux" && _env.arch == "arm64" {
		url: "https://github.com/BurntSushi/ripgrep/releases/download/14.1.1/ripgrep-14.1.1-aarch64-unknown-linux-gnu.tar.gz"
		checksum: value: "sha256:c827481c4ff4ea10c9dc7a4022c8de5db34a5737cb74484d62eb94a95841ab2f"
	}
	if _env.os == "linux" && _env.arch == "amd64" {
		url: "https://github.com/BurntSushi/ripgrep/releases/download/14.1.1/ripgrep-14.1.1-x86_64-unknown-linux-musl.tar.gz"
		checksum: value: "sha256:4ef94ea177ab67e4d555a2f9c4a9e9bb19eb2a44c50c03fa8d9d0dc48713242b"
	}
	if _env.os == "darwin" && _env.arch == "arm64" {
		url: "https://github.com/BurntSushi/ripgrep/releases/download/14.1.1/ripgrep-14.1.1-aarch64-apple-darwin.tar.gz"
		checksum: value: "sha256:b57b43ef71c20b22d8b409a4ae1320eb57c7d170cc8d66acf04e4f0bc1f8d3da"
	}
	if _env.os == "darwin" && _env.arch == "amd64" {
		url: "https://github.com/BurntSushi/ripgrep/releases/download/14.1.1/ripgrep-14.1.1-x86_64-apple-darwin.tar.gz"
		checksum: value: "sha256:3fdf6f7ab053786a0ae5ac07e4f088fee67e5bc2c11ef9225e9cefc1dc9d9826"
	}
}

_fdSource: {
	if _env.os == "linux" && _env.arch == "arm64" {
		url: "https://github.com/sharkdp/fd/releases/download/v10.2.0/fd-v10.2.0-aarch64-unknown-linux-gnu.tar.gz"
		checksum: value: "sha256:6de8be7a3d8ca27954a6d1e22bc327af4cf6fc7622791e68b820197f915c422b"
	}
	if _env.os == "linux" && _env.arch == "amd64" {
		url: "https://github.com/sharkdp/fd/releases/download/v10.2.0/fd-v10.2.0-x86_64-unknown-linux-gnu.tar.gz"
		checksum: value: "sha256:5f9030bcb0e1d03818521ed2e3d74fdb046480a45a4418ccff4f070241b4ed25"
	}
	if _env.os == "darwin" && _env.arch == "arm64" {
		url: "https://github.com/sharkdp/fd/releases/download/v10.2.0/fd-v10.2.0-aarch64-apple-darwin.tar.gz"
		checksum: value: "sha256:3d54e372d6aed13897e401c556f647195d77b0c59e4ecb1cc5bc9f0e3a59e6eb"
	}
	if _env.os == "darwin" && _env.arch == "amd64" {
		url: "https://github.com/sharkdp/fd/releases/download/v10.2.0/fd-v10.2.0-x86_64-apple-darwin.tar.gz"
		checksum: value: "sha256:8fc6d5a8d9ca98f116a3f214e8dc398c3d722c2c63eb324618d7a5efde59a7b4"
	}
}

_batSource: {
	if _env.os == "linux" && _env.arch == "arm64" {
		url: "https://github.com/sharkdp/bat/releases/download/v0.24.0/bat-v0.24.0-aarch64-unknown-linux-gnu.tar.gz"
		checksum: value: "sha256:feccae9a0576d97609c57e32d3914c5116136eab0df74c2ab74ef397d42c5b10"
	}
	if _env.os == "linux" && _env.arch == "amd64" {
		url: "https://github.com/sharkdp/bat/releases/download/v0.24.0/bat-v0.24.0-x86_64-unknown-linux-gnu.tar.gz"
		checksum: value: "sha256:0faf5d51b85bf81b92495dc93bf687d5c904adc9818b16f61ec2e7a4f925c77a"
	}
	if _env.os == "darwin" && _env.arch == "arm64" {
		url: "https://github.com/sharkdp/bat/releases/download/v0.24.0/bat-v0.24.0-aarch64-apple-darwin.tar.gz"
		checksum: value: "sha256:7d3478d6555a75b2c59edda1c4a95eb8a0fc2fc7e77dcab54dd2f6afb4cae23f"
	}
	if _env.os == "darwin" && _env.arch == "amd64" {
		url: "https://github.com/sharkdp/bat/releases/download/v0.24.0/bat-v0.24.0-x86_64-apple-darwin.tar.gz"
		checksum: value: "sha256:ce4a4f98738b06f7dc03c3d91e9a7231b6fcd7d59a50e40c51478d4c65b8f527"
	}
}

aquaInstaller: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "Installer"
	metadata: name: "aqua"
	spec: {
		type: "download"
	}
}

rg: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "rg"
	spec: {
		installerRef: "aqua"
		version:      "14.1.1"
		source:       _rgSource
	}
}

fd: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "fd"
	spec: {
		installerRef: "aqua"
		version:      "10.2.0"
		source:       _fdSource
	}
}

bat: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "bat"
	spec: {
		installerRef: "aqua"
		version:      "0.24.0"
		source:       _batSource
	}
}
