package tomei

// Go runtime WITHOUT taintOnUpgrade (default false).
// When upgraded, dependent tools should NOT be tainted.

_os:   string @tag(os)
_arch: string @tag(arch)

_goVersion: "1.25.6"

goRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Runtime"
	metadata: name: "go"
	spec: {
		type:    "download"
		version: _goVersion
		source: {
			url: "https://go.dev/dl/go\(spec.version).\(_os)-\(_arch).tar.gz"
			checksum: url: "https://go.dev/dl/?mode=json&include=all"
		}
		binaries: ["go", "gofmt"]
		binDir:      "~/go/bin"
		toolBinPath: "~/go/bin"
		env: {
			GOROOT: "~/.local/share/tomei/runtimes/go/\(spec.version)"
			GOBIN:  "~/go/bin"
		}
		// taintOnUpgrade is intentionally omitted (defaults to false)
		commands: {
			install: ["go install {{.Package}}@{{.Version}}"]
			remove: ["rm -f {{.BinPath}}"]
		}
	}
}
