package tomei

// Node.js tools installed via pnpm add -g
nodeTools: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "ToolSet"
	metadata: {
		name:        "node-tools"
		description: "Node.js CLI tools installed via pnpm"
	}
	spec: {
		runtimeRef: "pnpm"
		tools: {
			prettier: {package: "prettier", version: "3.8.1"}
			"ts-node": {package: "ts-node", version: "10.9.2"}
			typescript: {package: "typescript", version: "5.8.3"}
			"npm-check": {package: "npm-check-updates", version: "19.3.2"}
		}
	}
}
