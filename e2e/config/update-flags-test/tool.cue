package tomei

// Tool that depends on mock-rt runtime (delegation pattern).
// Used to test --update-runtimes cascade behavior.

mockTool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "mock-tool"
	spec: {
		runtimeRef: "mock-rt"
		package:    "mock-package"
		version:    "0.1.0"
	}
}
