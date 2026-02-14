package manifests

// 3-segment aqua registry package test: grafana/loki/logcli
// This uses github_release type which our resolver fully supports.
_logcliVersion: "v3.5.0"

logcli: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "logcli"
	spec: {
		installerRef: "aqua"
		version:      _logcliVersion
		package:      "grafana/loki/logcli"
	}
}
