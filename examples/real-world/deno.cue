package tomei

import "tomei.terassyi.net/presets/deno"

// Deno tools installed via deno install -g
denoTools: deno.#DenoToolSet & {
	metadata: name: "deno-tools"
	spec: tools: {
		deployctl: {package: "jsr:@deno/deployctl", version: "1.12.0"}
	}
}
