package brew

import "tomei.terassyi.net/schema"

// Homebrew prefix for macOS (Apple Silicon).
_brewPrefix: "/opt/homebrew"
_brew:       _brewPrefix + "/bin/brew"

// #Homebrew declares brew as a self-managed tool (commands pattern).
// Darwin/arm64 only — guarded by @if(darwin) in the manifest file.
//
// Usage:
//   homebrew: brew.#Homebrew
#Homebrew: schema.#Tool & {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: {
		name:        "homebrew"
		description: string | *"Homebrew package manager"
	}
	spec: commands: {
		install: [
			"NONINTERACTIVE=1 /bin/bash -c \"$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)\"",
		]
		check: [_brew + " --version"]
		remove: ["NONINTERACTIVE=1 /bin/bash -c \"$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/uninstall.sh)\""]
		resolveVersion: [_brew + " --version 2>/dev/null | head -1 | grep -oE '[0-9]+\\.[0-9]+\\.[0-9]+' || echo ''"]
	}
}

// #BrewInstaller declares the brew delegation installer.
// Depends on #Homebrew being present (toolRef: "homebrew").
// Homebrew's install script adds brew to PATH, so installer commands
// use bare "brew". binDir is set for tomei env PATH inclusion.
//
// Usage:
//   brewInstaller: brew.#BrewInstaller
#BrewInstaller: schema.#Installer & {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Installer"
	metadata: {
		name:        "brew"
		description: string | *"Install packages via Homebrew"
	}
	spec: {
		type:    "delegation"
		toolRef: "homebrew"
		commands: {
			install: ["brew install {{.Package}}"]
			remove: ["brew uninstall {{.Package}}"]
			check: ["brew list --formula {{.Package}} >/dev/null 2>&1"]
		}
		binDir: _brewPrefix + "/bin"
	}
}

// #Formula declares a single Homebrew formula.
// version is informational — brew does not support universal version pinning.
//
// Usage:
//   jq: brew.#Formula & { metadata: name: "jq", spec: package: "jq" }
#Formula: schema.#Tool & {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: {
		name:         string
		description?: string
	}
	spec: {
		installerRef: "brew"
		package:      string & !=""
		version?:     string
	}
}

// #FormulaSet declares a set of Homebrew formulae.
// Requires #Homebrew and #BrewInstaller to be declared.
//
// Usage:
//   brewTools: brew.#FormulaSet & {
//       metadata: name: "brew-tools"
//       spec: tools: {
//           tree: {package: "tree"}
//           wget: {package: "wget"}
//       }
//   }
#FormulaSet: schema.#ToolSet & {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "ToolSet"
	metadata: {
		name:        string
		description: string | *"Homebrew formulae"
	}
	spec: {
		installerRef: "brew"
		tools: {[string]: {
			package:  string & !=""
			version?: string
		}}
	}
}
