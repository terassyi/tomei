package tomei

// Python tools installed via uv tool install
uvTools: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "ToolSet"
	metadata: {
		name:        "uv-tools"
		description: "Python CLI tools installed via uv"
	}
	spec: {
		runtimeRef: "uv"
		tools: {
			ruff: {package: "ruff", version: "0.15.1"}
			mypy: {package: "mypy", version: "1.19.1"}
			httpie: {package: "httpie", version: "3.2.4"}
			ansible: {package: "ansible", version: "13.3.0", args: ["--with-executables-from", "ansible-core"]}
		}
	}
}
