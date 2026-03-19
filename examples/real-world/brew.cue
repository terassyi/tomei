@if(darwin)

package tomei

import "tomei.terassyi.net/presets/brew"

homebrew: brew.#Homebrew & {
	platform: {os: _os, arch: _arch}
}

brewInstaller: brew.#BrewInstaller & {
	platform: {os: _os, arch: _arch}
}

brewTools: brew.#FormulaSet & {
	metadata: {
		name:        "brew-formulae"
		description: "Common Homebrew formulae"
	}
	spec: tools: {
		tree: {package: "tree"}
		wget: {package: "wget"}
	}
}
