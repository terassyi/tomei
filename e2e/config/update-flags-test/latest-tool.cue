package tomei

// Tool with "latest" version for testing --update-tools.
// Since the version is empty (latest), --update-tools should taint and reinstall it.

latestTool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "latest-mock-tool"
	spec: {
		runtimeRef: "mock-rt"
		package:    "latest-mock-package"
	}
}
