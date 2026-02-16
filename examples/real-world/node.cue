package tomei

// Node.js tools installed via pnpm add -g
pnpmTools: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "ToolSet"
	metadata: {
		name:        "pnpm-tools"
		description: "Node.js CLI tools installed via pnpm"
	}
	spec: {
		runtimeRef: "pnpm"
		tools: {
			prettier: {package: "prettier", version: "3.5.3"}
			"ts-node": {package: "ts-node", version: "10.9.2"}
			typescript: {package: "typescript", version: "5.7.3"}
			"npm-check-updates": {package: "npm-check-updates", version: "17.1.14"}
		}
	}
}
