package toto

gh: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "gh"
	spec: {
		installerRef: "download"
		version:      "2.86.0"
		source: {
			url: "https://github.com/cli/cli/releases/download/v2.86.0/gh_2.86.0_linux_amd64.tar.gz"
			checksum: {
				url: "https://github.com/cli/cli/releases/download/v2.86.0/gh_2.86.0_checksums.txt"
			}
		}
	}
}
