package tomei

import "tomei.terassyi.net/presets/node"

// Node.js tools installed via pnpm add -g
pnpmTools: node.#PnpmToolSet & {
	metadata: name: "pnpm-tools"
	spec: tools: {
		prettier: {package: "prettier", version: "3.5.3"}
		"ts-node": {package: "ts-node", version: "10.9.2"}
		typescript: {package: "typescript", version: "5.7.3"}
		"npm-check-updates": {package: "npm-check-updates", version: "17.1.14"}
	}
}
