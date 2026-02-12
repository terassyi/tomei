package tomei

// gh CLI tool - uses @tag(os)/@tag(arch) for OS/arch portability

_os:   string @tag(os)
_arch: string @tag(arch)

_ghVersion: "2.86.0"

gh: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "gh"
	spec: {
		installerRef: "download"
		version:      _ghVersion
		source: {
			if _os == "linux" {
				url: "https://github.com/cli/cli/releases/download/v\(spec.version)/gh_\(spec.version)_linux_\(_arch).tar.gz"
			}
			if _os == "darwin" {
				url: "https://github.com/cli/cli/releases/download/v\(spec.version)/gh_\(spec.version)_macOS_\(_arch).zip"
			}
			checksum: url: "https://github.com/cli/cli/releases/download/v\(spec.version)/gh_\(spec.version)_checksums.txt"
		}
	}
}
