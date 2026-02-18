package bun

import "tomei.terassyi.net/schema"

// #BunRuntime declares a Bun runtime installed from GitHub releases.
// User provides spec.version and platform.
//
// Usage:
//   bunRuntime: #BunRuntime & {
//       platform: { os: _os, arch: _arch }
//       spec: version: "1.2.21"
//   }
#BunRuntime: schema.#Runtime & {
	let _binDir = "~/.bun/bin"
	let _bun = _binDir + "/bun"

	platform: {
		os:   string
		arch: string
	}

	let _archMap = {
		amd64: "x64"
		arm64: "aarch64"
	}
	let _target = "bun-" + platform.os + "-" + _archMap[platform.arch]

	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Runtime"
	metadata: {
		name:        "bun"
		description: string | *"Bun JavaScript/TypeScript runtime"
	}
	spec: {
		type:    "download"
		version: string & !=""
		source: {
			url:         "https://github.com/oven-sh/bun/releases/download/bun-v\(spec.version)/\(_target).zip"
			archiveType: "zip"
		}
		binaries: ["bun"]
		binDir:      _binDir
		toolBinPath: _binDir
		commands: {
			install: ["\(_bun) install -g {{.Package}}{{if .Version}}@{{.Version}}{{end}}"]
			remove: ["rm -f {{.BinPath}}"]
		}
	}
}

// #BunTool declares a single tool installed via bun install -g.
//
// Usage:
//   myTool: #BunTool & {
//       metadata: name: "my-tool"
//       spec: {
//           package: "my-tool"
//           version: "1.0.0"
//       }
//   }
#BunTool: schema.#Tool & {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: {
		name:         string
		description?: string
	}
	spec: {
		runtimeRef: "bun"
		package:    string & !=""
		version:    string & !=""
	}
}

// #BunToolSet declares a set of tools installed via bun install -g.
//
// Usage:
//   bunTools: #BunToolSet & {
//       metadata: name: "bun-tools"
//       spec: tools: {
//           prettier: {package: "prettier", version: "3.5.0"}
//       }
//   }
#BunToolSet: schema.#ToolSet & {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "ToolSet"
	metadata: {
		name:        string
		description: string | *"Tools installed via bun install"
	}
	spec: {
		runtimeRef: "bun"
		tools: {[string]: {
			package: string & !=""
			version: string & !=""
		}}
	}
}
