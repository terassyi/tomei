package toto

// Parallel tool installation test: multiple independent tools
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
		source: {
			url: "https://github.com/BurntSushi/ripgrep/releases/download/14.1.1/ripgrep-14.1.1-aarch64-unknown-linux-gnu.tar.gz"
			checksum: {
				value: "sha256:c827481c4ff4ea10c9dc7a4022c8de5db34a5737cb74484d62eb94a95841ab2f"
			}
		}
	}
}

fd: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "fd"
	spec: {
		installerRef: "aqua"
		version:      "10.2.0"
		source: {
			url: "https://github.com/sharkdp/fd/releases/download/v10.2.0/fd-v10.2.0-aarch64-unknown-linux-gnu.tar.gz"
			checksum: {
				value: "sha256:6de8be7a3d8ca27954a6d1e22bc327af4cf6fc7622791e68b820197f915c422b"
			}
		}
	}
}

bat: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "bat"
	spec: {
		installerRef: "aqua"
		version:      "0.24.0"
		source: {
			url: "https://github.com/sharkdp/bat/releases/download/v0.24.0/bat-v0.24.0-aarch64-unknown-linux-gnu.tar.gz"
			checksum: {
				value: "sha256:feccae9a0576d97609c57e32d3914c5116136eab0df74c2ab74ef397d42c5b10"
			}
		}
	}
}
