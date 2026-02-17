package tomei

import (
	gopreset "tomei.terassyi.net/presets/go"
	"tomei.terassyi.net/presets/rust"
)

// ---------------------------------------------------------------------------
// Go runtime (download from go.dev)
// ---------------------------------------------------------------------------

goRuntime: gopreset.#GoRuntime & {
	platform: {os: _os, arch: _arch}
	spec: version: "1.26.0"
}

// ---------------------------------------------------------------------------
// Rust runtime (delegation via rustup)
// ---------------------------------------------------------------------------

rustRuntime: rust.#RustRuntime & {
	spec: version: "stable"
}

// ---------------------------------------------------------------------------
// uv runtime (delegation — standalone installer)
// ---------------------------------------------------------------------------

uvRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Runtime"
	metadata: {
		name:        "uv"
		description: "Python package manager and project tool"
	}
	spec: {
		type:    "delegation"
		version: "0.10.2"
		bootstrap: {
			install: ["curl -LsSf https://astral.sh/uv/{{.Version}}/install.sh | sh"]
			check: ["~/.local/bin/uv --version"]
			remove: ["~/.local/bin/uv self uninstall"]
			resolveVersion: ["~/.local/bin/uv --version 2>/dev/null | grep -oP '\\d+\\.\\d+\\.\\d+' || echo ''"]
		}
		binaries: ["uv", "uvx"]
		binDir:      "~/.local/bin"
		toolBinPath: "~/.local/bin"
		commands: {
			install: ["~/.local/bin/uv tool install {{.Package}}{{if .Version}}=={{.Version}}{{end}}{{if .Args}} {{.Args}}{{end}}"]
			remove: ["~/.local/bin/uv tool uninstall {{.Package}}"]
		}
	}
}

// ---------------------------------------------------------------------------
// Node.js runtime (delegation via pnpm standalone installer)
// ---------------------------------------------------------------------------

pnpmRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Runtime"
	metadata: {
		name:        "pnpm"
		description: "Node.js package manager (standalone, manages Node.js via pnpm env)"
	}
	spec: {
		type:    "delegation"
		version: "10.29.3"
		bootstrap: {
			install: [
				"curl -fsSL https://get.pnpm.io/install.sh | SHELL=/bin/bash PNPM_VERSION={{.Version}} sh -",
				"export PNPM_HOME=$HOME/.local/share/pnpm",
				"export PATH=$PNPM_HOME:$PATH",
				"$PNPM_HOME/pnpm env use --global lts",
			]
			check: ["~/.local/share/pnpm/pnpm --version"]
			remove: ["rm -rf ~/.local/share/pnpm"]
			resolveVersion: ["~/.local/share/pnpm/pnpm --version 2>/dev/null || echo ''"]
		}
		binaries: ["pnpm", "pnpx"]
		binDir:      "~/.local/share/pnpm"
		toolBinPath: "~/.local/share/pnpm"
		env: {
			PNPM_HOME: "~/.local/share/pnpm"
		}
		commands: {
			install: ["~/.local/share/pnpm/pnpm add -g {{.Package}}{{if .Version}}@{{.Version}}{{end}}"]
			remove: ["~/.local/share/pnpm/pnpm remove -g {{.Package}}"]
		}
	}
}
