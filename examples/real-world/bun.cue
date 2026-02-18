package tomei

import "tomei.terassyi.net/presets/bun"

// Bun tools installed via bun install -g
bunTools: bun.#BunToolSet & {
	metadata: name: "bun-tools"
	spec: tools: {
		biome: {package: "@biomejs/biome", version: "1.9.4"}
	}
}
