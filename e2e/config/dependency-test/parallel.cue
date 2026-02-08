package tomei

// Parallel tool installation test: multiple independent tools

_rgVersion: "14.1.1"
_fdVersion: "10.2.0"
_batVersion: "0.26.1"

// Architecture mapping for URL patterns
_archMap: {
	arm64: "aarch64"
	amd64: "x86_64"
}

// ripgrep: linux-gnu uses gnu, linux-amd64 uses musl (different suffix)
_rgSource: {
	if _env.os == "linux" && _env.arch == "arm64" {
		url: "https://github.com/BurntSushi/ripgrep/releases/download/\(_rgVersion)/ripgrep-\(_rgVersion)-\(_archMap[_env.arch])-unknown-linux-gnu.tar.gz"
		checksum: value: "sha256:c827481c4ff4ea10c9dc7a4022c8de5db34a5737cb74484d62eb94a95841ab2f"
	}
	if _env.os == "linux" && _env.arch == "amd64" {
		url: "https://github.com/BurntSushi/ripgrep/releases/download/\(_rgVersion)/ripgrep-\(_rgVersion)-\(_archMap[_env.arch])-unknown-linux-musl.tar.gz"
		checksum: value: "sha256:4cf9f2741e6c465ffdb7c26f38056a59e2a2544b51f7cc128ef28337eeae4d8e"
	}
	if _env.os == "darwin" {
		url: "https://github.com/BurntSushi/ripgrep/releases/download/\(_rgVersion)/ripgrep-\(_rgVersion)-\(_archMap[_env.arch])-apple-darwin.tar.gz"
	}
	if _env.os == "darwin" && _env.arch == "arm64" {
		checksum: value: "sha256:24ad76777745fbff131c8fbc466742b011f925bfa4fffa2ded6def23b5b937be"
	}
	if _env.os == "darwin" && _env.arch == "amd64" {
		checksum: value: "sha256:fc87e78f7cb3fea12d69072e7ef3b21509754717b746368fd40d88963630e2b3"
	}
}

// fd: consistent pattern across all platforms
_fdSource: {
	if _env.os == "linux" {
		url: "https://github.com/sharkdp/fd/releases/download/v\(_fdVersion)/fd-v\(_fdVersion)-\(_archMap[_env.arch])-unknown-linux-gnu.tar.gz"
	}
	if _env.os == "darwin" {
		url: "https://github.com/sharkdp/fd/releases/download/v\(_fdVersion)/fd-v\(_fdVersion)-\(_archMap[_env.arch])-apple-darwin.tar.gz"
	}
	if _env.os == "linux" && _env.arch == "arm64" {
		checksum: value: "sha256:6de8be7a3d8ca27954a6d1e22bc327af4cf6fc7622791e68b820197f915c422b"
	}
	if _env.os == "linux" && _env.arch == "amd64" {
		checksum: value: "sha256:5f9030bcb0e1d03818521ed2e3d74fdb046480a45a4418ccff4f070241b4ed25"
	}
	if _env.os == "darwin" && _env.arch == "arm64" {
		checksum: value: "sha256:ae6327ba8c9a487cd63edd8bddd97da0207887a66d61e067dfe80c1430c5ae36"
	}
	if _env.os == "darwin" && _env.arch == "amd64" {
		checksum: value: "sha256:991a648a58870230af9547c1ae33e72cb5c5199a622fe5e540e162d6dba82d48"
	}
}

// bat: consistent pattern across all platforms
_batSource: {
	if _env.os == "linux" {
		url: "https://github.com/sharkdp/bat/releases/download/v\(_batVersion)/bat-v\(_batVersion)-\(_archMap[_env.arch])-unknown-linux-gnu.tar.gz"
	}
	if _env.os == "darwin" {
		url: "https://github.com/sharkdp/bat/releases/download/v\(_batVersion)/bat-v\(_batVersion)-\(_archMap[_env.arch])-apple-darwin.tar.gz"
	}
	if _env.os == "linux" && _env.arch == "arm64" {
		checksum: value: "sha256:422eb73e11c854fddd99f5ca8461c2f1d6e6dce0a2a8c3d5daade5ffcb6564aa"
	}
	if _env.os == "linux" && _env.arch == "amd64" {
		checksum: value: "sha256:726f04c8f576a7fd18b7634f1bbf2f915c43494c1c0f013baa3287edb0d5a2a3"
	}
	if _env.os == "darwin" && _env.arch == "arm64" {
		checksum: value: "sha256:e30beff26779c9bf60bb541e1d79046250cb74378f2757f8eb250afddb19e114"
	}
	if _env.os == "darwin" && _env.arch == "amd64" {
		checksum: value: "sha256:830d63b0bba1fa040542ec569e3cf77f60d3356b9de75116a344b061e0894245"
	}
}

aquaInstaller: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Installer"
	metadata: name: "aqua"
	spec: {
		type: "download"
	}
}

rg: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "rg"
	spec: {
		installerRef: "aqua"
		version:      _rgVersion
		source:       _rgSource
	}
}

fd: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "fd"
	spec: {
		installerRef: "aqua"
		version:      _fdVersion
		source:       _fdSource
	}
}

bat: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "bat"
	spec: {
		installerRef: "aqua"
		version:      _batVersion
		source:       _batSource
	}
}
