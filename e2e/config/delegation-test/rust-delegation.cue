package tomei

// sd - intuitive find & replace CLI installed via cargo install (Runtime Delegation)

_sdVersion: "1.0.0"

sd: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "sd"
	spec: {
		runtimeRef: "rust"
		package:    "sd"
		version:    _sdVersion
	}
}
