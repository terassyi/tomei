package node

import "tomei.terassyi.net/schema"

// #PnpmRuntime declares a Node.js runtime managed via pnpm standalone installer.
// pnpm bootstraps itself and provisions a global Node.js LTS via `pnpm env use`.
//
// Usage:
//   pnpmRuntime: #PnpmRuntime
//   pnpmRuntime: #PnpmRuntime & {spec: version: "10.29.3"}
#PnpmRuntime: schema.#Runtime & {
	let _pnpmHome = "~/.local/share/pnpm"
	let _pnpm = _pnpmHome + "/pnpm"

	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Runtime"
	metadata: {
		name:        "pnpm"
		description: string | *"Node.js package manager (standalone, manages Node.js via pnpm env)"
	}
	spec: {
		type:    "delegation"
		version: string | *"latest"
		bootstrap: {
			install: [
				"curl -fsSL https://get.pnpm.io/install.sh | SHELL=/bin/bash PNPM_VERSION={{.Version}} sh -",
				"export PNPM_HOME=$HOME/.local/share/pnpm",
				"export PATH=$PNPM_HOME:$PATH",
				"$PNPM_HOME/pnpm env use --global lts",
			]
			check: ["\(_pnpm) --version"]
			remove: ["rm -rf \(_pnpmHome)"]
			resolveVersion: ["\(_pnpm) --version 2>/dev/null || echo ''"]
		}
		binaries: ["pnpm", "pnpx"]
		binDir:      _pnpmHome
		toolBinPath: _pnpmHome
		env: {
			PNPM_HOME: _pnpmHome
		}
		commands: {
			install: ["\(_pnpm) add -g {{.Package}}{{if .Version}}@{{.Version}}{{end}}"]
			remove: ["\(_pnpm) remove -g {{.Package}}"]
		}
	}
}

// #PnpmTool declares a single tool installed via pnpm add -g.
//
// Usage:
//   prettier: #PnpmTool & {
//       metadata: name: "prettier"
//       spec: {
//           package: "prettier"
//           version: "3.5.3"
//       }
//   }
#PnpmTool: schema.#Tool & {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: {
		name:         string
		description?: string
	}
	spec: {
		runtimeRef: "pnpm"
		package:    string & !=""
		version:    string & !=""
	}
}

// #PnpmToolSet declares a set of tools installed via pnpm add -g.
//
// Usage:
//   nodeTools: #PnpmToolSet & {
//       metadata: name: "node-tools"
//       spec: tools: {
//           prettier:   {package: "prettier", version: "3.5.3"}
//           typescript: {package: "typescript", version: "5.7.3"}
//       }
//   }
#PnpmToolSet: schema.#ToolSet & {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "ToolSet"
	metadata: {
		name:        string
		description: string | *"Node.js tools installed via pnpm"
	}
	spec: {
		runtimeRef: "pnpm"
		tools: {[string]: {
			package: string & !=""
			version: string & !=""
		}}
	}
}
