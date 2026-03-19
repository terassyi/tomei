package brew

import "tomei.terassyi.net/schema"

// Package-level shared prefix map (hidden, not exported)
_brewPrefixMap: {
	darwin: {arm64: "/opt/homebrew", amd64: "/usr/local"}
	linux: {arm64: "/home/linuxbrew/.linuxbrew", amd64: "/home/linuxbrew/.linuxbrew"}
}

// #Homebrew declares brew as a self-managed tool (commands pattern).
// Requires platform to compute the correct brew prefix.
//
// Usage:
//   homebrew: brew.#Homebrew & { platform: {os: _os, arch: _arch} }
#Homebrew: schema.#Tool & {
	platform: {
		os:   "darwin" | "linux"
		arch: "amd64" | "arm64"
	}
	let _prefix = _brewPrefixMap[platform.os][platform.arch]
	let _brew = _prefix + "/bin/brew"

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
// Uses full path to brew binary — no PATH injection needed.
//
// Usage:
//   brewInstaller: brew.#BrewInstaller & { platform: {os: _os, arch: _arch} }
#BrewInstaller: schema.#Installer & {
	platform: {
		os:   "darwin" | "linux"
		arch: "amd64" | "arm64"
	}
	let _prefix = _brewPrefixMap[platform.os][platform.arch]
	let _brew = _prefix + "/bin/brew"

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
			install: [_brew + " install {{.Package}}"]
			remove: [_brew + " uninstall {{.Package}}"]
			check: [_brew + " list --formula {{.Package}} >/dev/null 2>&1"]
		}
		binDir: _prefix + "/bin"
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
