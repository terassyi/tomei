@if(darwin)

package tomei

import "tomei.terassyi.net/presets/brew"

homebrew: brew.#Homebrew

brewInstaller: brew.#BrewInstaller

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
