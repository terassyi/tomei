package tomei

// Parallel execution failure test:
// One tool fails (delegation with exit 1), another succeeds (rg via download).
// The successful tool should still be installed despite the failure.

_os:   string @tag(os)
_arch: string @tag(arch)

_rgVersion: "14.1.1"

_archMap: {
	arm64: "aarch64"
	amd64: "x86_64"
}

_rgSource: {
	if _os == "linux" && _arch == "arm64" {
		url: "https://github.com/BurntSushi/ripgrep/releases/download/\(_rgVersion)/ripgrep-\(_rgVersion)-\(_archMap[_arch])-unknown-linux-gnu.tar.gz"
		checksum: value: "sha256:c827481c4ff4ea10c9dc7a4022c8de5db34a5737cb74484d62eb94a95841ab2f"
	}
	if _os == "linux" && _arch == "amd64" {
		url: "https://github.com/BurntSushi/ripgrep/releases/download/\(_rgVersion)/ripgrep-\(_rgVersion)-\(_archMap[_arch])-unknown-linux-musl.tar.gz"
		checksum: value: "sha256:4cf9f2741e6c465ffdb7c26f38056a59e2a2544b51f7cc128ef28337eeae4d8e"
	}
	if _os == "darwin" {
		url: "https://github.com/BurntSushi/ripgrep/releases/download/\(_rgVersion)/ripgrep-\(_rgVersion)-\(_archMap[_arch])-apple-darwin.tar.gz"
	}
	if _os == "darwin" && _arch == "arm64" {
		checksum: value: "sha256:24ad76777745fbff131c8fbc466742b011f925bfa4fffa2ded6def23b5b937be"
	}
	if _os == "darwin" && _arch == "amd64" {
		checksum: value: "sha256:fc87e78f7cb3fea12d69072e7ef3b21509754717b746368fd40d88963630e2b3"
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

// Deliberately failing installer (delegation with exit 1)
failInstaller: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Installer"
	metadata: name: "fail-installer"
	spec: {
		type: "delegation"
		commands: {
			install: ["echo 'simulated failure'", "exit 1"]
			remove: ["echo removing"]
		}
	}
}

// This tool should succeed
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

// This tool should fail
failTool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "fail-tool"
	spec: {
		installerRef: "fail-installer"
		package:      "fake-package"
		version:      "1.0.0"
	}
}
