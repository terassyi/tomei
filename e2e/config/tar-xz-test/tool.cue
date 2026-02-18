package tomei

// Tool using tar.xz archive — validates that tomei correctly detects
// and handles .tar.xz URLs via automatic archive type detection.

_os:   string @tag(os)
_arch: string @tag(arch)

_zigVersion: "0.14.0"

_archMap: {
	amd64: "x86_64"
	arm64: "aarch64"
}
_osMap: {
	linux:  "linux"
	darwin: "macos"
}

zig: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: {
		name:        "zig"
		description: "Zig programming language compiler"
	}
	spec: {
		installerRef: "download"
		version:      _zigVersion
		source: url: "https://ziglang.org/download/\(_zigVersion)/zig-\(_osMap[_os])-\(_archMap[_arch])-\(_zigVersion).tar.xz"
	}
}
