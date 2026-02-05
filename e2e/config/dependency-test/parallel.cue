package toto

// Parallel tool installation test: multiple independent tools using aqua registry
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
		package: {
			registry: "aqua"
			name:     "BurntSushi/ripgrep"
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
		package: {
			registry: "aqua"
			name:     "sharkdp/fd"
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
		package: {
			registry: "aqua"
			name:     "sharkdp/bat"
		}
	}
}
