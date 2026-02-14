package tomei

import "tomei.terassyi.net/presets/aqua"

// Common CLI utility tools installed via aqua registry
utilityTools: aqua.#AquaToolSet & {
	metadata: {
		name:        "utility-tools"
		description: "Common CLI utilities for daily development"
	}
	spec: tools: {
		bat: {package: "sharkdp/bat", version: "v0.26.0"}
		rg: {package: "BurntSushi/ripgrep", version: "15.1.0"}
		fd: {package: "sharkdp/fd", version: "v10.3.0"}
		jq: {package: "jqlang/jq", version: "1.8.1"}
		yq: {package: "mikefarah/yq", version: "v4.52.2"}
		fzf: {package: "junegunn/fzf", version: "v0.67.0"}
	}
}
