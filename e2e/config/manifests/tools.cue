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
			if _env.os == "linux" {
				url: "https://github.com/cli/cli/releases/download/v\(spec.version)/gh_\(spec.version)_linux_\(_env.arch).tar.gz"
			}
			if _env.os == "darwin" {
				url: "https://github.com/cli/cli/releases/download/v\(spec.version)/gh_\(spec.version)_macOS_\(_env.arch).zip"
			}
			checksum: url: "https://github.com/cli/cli/releases/download/v\(spec.version)/gh_\(spec.version)_checksums.txt"
		}
	}
}
