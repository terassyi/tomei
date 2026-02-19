package tomei

// Download-type runtime using http-text resolver.
// version "latest" triggers resolveVersion to fetch version from HTTP endpoint.
httpTextRT: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Runtime"
	metadata: name: "http-text-rt"
	spec: {
		type:           "download"
		version:        "latest"
		resolveVersion: ["http-text:http://localhost:18888/version.txt:^v(.+)"]
		source: {
			url:         "http://localhost:18888/archive/http-text-rt-v{{.Version}}.tar.gz"
			archiveType: "tar.gz"
		}
		binaries: ["http-text-rt"]
		binDir:      "/tmp/http-text-rt-bin"
		toolBinPath: "/tmp/http-text-rt-bin"
	}
}
