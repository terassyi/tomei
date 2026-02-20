package python

import "tomei.terassyi.net/schema"

// #UvRuntime declares a Python tool/runtime manager installed via the uv standalone installer.
//
// Usage:
//   uvRuntime: #UvRuntime
//   uvRuntime: #UvRuntime & {spec: version: "0.10.2"}
#UvRuntime: schema.#Runtime & {
	let _binDir = "~/.local/bin"
	let _uv = _binDir + "/uv"

	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Runtime"
	metadata: {
		name:        "uv"
		description: string | *"Python package manager and project tool"
	}
	spec: {
		type:    "delegation"
		version: string | *"latest"
		bootstrap: {
			install: ["curl -LsSf https://astral.sh/uv/{{.Version}}/install.sh | sh"]
			update: ["\(_uv) self update{{if ne .Version \"latest\"}} {{.Version}}{{end}}"]
			check: ["\(_uv) --version"]
			remove: ["\(_uv) self uninstall"]
			resolveVersion: ["\(_uv) --version 2>/dev/null | grep -oP '\\d+\\.\\d+\\.\\d+' || echo ''"]
		}
		binaries: ["uv", "uvx"]
		binDir:      _binDir
		toolBinPath: _binDir
		commands: {
			install: ["\(_uv) tool install {{.Package}}{{if .Version}}=={{.Version}}{{end}}{{if .Args}} {{.Args}}{{end}}"]
			remove: ["\(_uv) tool uninstall {{.Package}}"]
		}
	}
}

// #UvTool declares a single Python tool installed via uv tool install.
//
// Usage:
//   ruff: #UvTool & {
//       metadata: name: "ruff"
//       spec: {
//           package: "ruff"
//           version: "0.15.1"
//       }
//   }
#UvTool: schema.#Tool & {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: {
		name:         string
		description?: string
	}
	spec: {
		runtimeRef: "uv"
		package:    string & !=""
		version?:   string
	}
}

// #UvToolSet declares a set of Python tools installed via uv tool install.
// Supports the args field for tools that require extra flags (e.g. ansible
// with --with-executables-from).
//
// Usage:
//   pythonTools: #UvToolSet & {
//       metadata: name: "python-tools"
//       spec: tools: {
//           ruff:    {package: "ruff", version: "0.15.1"}
//           ansible: {package: "ansible", version: "13.3.0", args: ["--with-executables-from", "ansible-core"]}
//       }
//   }
#UvToolSet: schema.#ToolSet & {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "ToolSet"
	metadata: {
		name:        string
		description: string | *"Python tools installed via uv"
	}
	spec: {
		runtimeRef: "uv"
		tools: {[string]: {
			package:  string & !=""
			version?: string
			args?: [...string]
		}}
	}
}
