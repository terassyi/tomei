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
			url: "https://github.com/cli/cli/releases/download/v\(spec.version)/gh_\(spec.version)_\(_env.os)_\(_env.arch).tar.gz"
			checksum: {
				url: "https://github.com/cli/cli/releases/download/v\(spec.version)/gh_\(spec.version)_checksums.txt"
			}
		}
	}
}
