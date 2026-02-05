package toto

// gh CLI tool - uses _env for OS/arch portability
gh: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "gh"
	spec: {
		installerRef: "download"
		version:      "2.86.0"
		source: {
			url: "https://github.com/cli/cli/releases/download/v2.86.0/gh_2.86.0_\(_env.os)_\(_env.arch).tar.gz"
			checksum: {
				url: "https://github.com/cli/cli/releases/download/v2.86.0/gh_2.86.0_checksums.txt"
			}
		}
	}
}
