package toto

// Reduced manifest for removal test:
// Only Tool/helm (no InstallerRepository, no common-chart)
// Used to test that applying this after a full manifest removes bitnami repo and common-chart.

// Helm tool installed via aqua registry (latest version)
helmTool: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "helm"
	spec: {
		installerRef: "aqua"
		package:      "helm/helm"
	}
}
