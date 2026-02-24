package tomei

import "tomei.terassyi.net/presets/aqua"

utilityTools: aqua.#AquaToolSet & {
	metadata: {
		name:        "utility-tools"
		description: "Common CLI utilities"
	}
	spec: tools: {
		rg:  {package: "BurntSushi/ripgrep", version: "15.1.0"}
		jq:  {package: "jqlang/jq", version: "1.8.1"}
		bat: {package: "sharkdp/bat", version: "v0.26.1"}
	}
}
