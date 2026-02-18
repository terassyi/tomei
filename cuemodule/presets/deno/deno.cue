package deno

import "tomei.terassyi.net/schema"

// #DenoRuntime declares a Deno runtime installed from dl.deno.land.
// User provides spec.version and platform.
//
// Usage:
//   denoRuntime: #DenoRuntime & {
//       platform: { os: _os, arch: _arch }
//       spec: version: "2.6.10"
//   }
#DenoRuntime: schema.#Runtime & {
	let _binDir = "~/.deno/bin"
	let _deno = _binDir + "/deno"

	platform: {
		os:   string
		arch: string
	}

	let _archMap = {
		amd64: "x86_64"
		arm64: "aarch64"
	}
	let _osMap = {
		linux:  "unknown-linux-gnu"
		darwin: "apple-darwin"
	}
	let _target = _archMap[platform.arch] + "-" + _osMap[platform.os]

	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Runtime"
	metadata: {
		name:        "deno"
		description: string | *"Deno JavaScript/TypeScript runtime"
	}
	spec: {
		type:    "download"
		version: string & !=""
		source: {
			url:         "https://dl.deno.land/release/v\(spec.version)/deno-\(_target).zip"
			archiveType: "zip"
		}
		binaries: ["deno"]
		binDir:      _binDir
		toolBinPath: _binDir
		commands: {
			install: ["\(_deno) install -g {{.Package}}{{if .Version}}@{{.Version}}{{end}}"]
			remove: ["rm -f {{.BinPath}}"]
		}
	}
}

// #DenoTool declares a single tool installed via deno install -g.
//
// Usage:
//   myTool: #DenoTool & {
//       metadata: name: "my-tool"
//       spec: {
//           package: "npm:my-tool"
//           version: "1.0.0"
//       }
//   }
#DenoTool: schema.#Tool & {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: {
		name:         string
		description?: string
	}
	spec: {
		runtimeRef: "deno"
		package:    string & !=""
		version:    string & !=""
	}
}

// #DenoToolSet declares a set of tools installed via deno install -g.
//
// Usage:
//   denoTools: #DenoToolSet & {
//       metadata: name: "deno-tools"
//       spec: tools: {
//           deployctl: {package: "jsr:@deno/deployctl", version: "1.12.0"}
//       }
//   }
#DenoToolSet: schema.#ToolSet & {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "ToolSet"
	metadata: {
		name:        string
		description: string | *"Tools installed via deno install"
	}
	spec: {
		runtimeRef: "deno"
		tools: {[string]: {
			package: string & !=""
			version: string & !=""
		}}
	}
}
