package go

import "tomei.terassyi.net/schema"

// #GoRuntime declares a Go runtime installed from go.dev.
// When spec.version is set to an exact version string (e.g., "1.26.0"),
// the resolveVersion step is skipped and the version is used as-is.
// When spec.version is omitted (defaults to "latest"), the latest
// stable version is automatically resolved from go.dev.
//
// Usage (pinned):
//   goRuntime: #GoRuntime & {
//       platform: { os: _os, arch: _arch }
//       spec: version: "1.26.0"
//   }
//
// Usage (latest):
//   goRuntime: #GoRuntime & {
//       platform: { os: _os, arch: _arch }
//   }
#GoRuntime: schema.#Runtime & {
	let _goBin = "~/go/bin"

	platform: {
		os:   string
		arch: string
	}
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Runtime"
	metadata: {
		name:        "go"
		description: string | *"Go programming language runtime"
	}
	spec: {
		type:    "download"
		version: string | *"latest"
		resolveVersion: ["http-text:https://go.dev/VERSION?m=text:^go(.+)"]
		source: {
			url: "https://go.dev/dl/go{{.Version}}.\(platform.os)-\(platform.arch).tar.gz"
			checksum: url: "https://go.dev/dl/?mode=json&include=all"
		}
		binaries: ["go", "gofmt"]
		binDir:      _goBin
		toolBinPath: _goBin
		env: {
			GOROOT: "~/.local/share/tomei/runtimes/go/{{.Version}}"
			GOBIN:  _goBin
		}
		commands: {
			install: ["go install {{.Package}}@{{.Version}}"]
			remove: ["rm -f {{.BinPath}}"]
		}
		taintOnUpgrade: true
	}
}

// #GoTool declares a single tool installed via go install.
//
// Usage:
//   myTool: #GoTool & {
//       metadata: name: "my-tool"
//       spec: {
//           package: "example.com/cmd/tool"
//           version: "v1.0.0"
//       }
//   }
#GoTool: schema.#Tool & {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: {
		name:         string
		description?: string
	}
	spec: {
		runtimeRef: "go"
		package:    string & !=""
		version:    string & !=""
	}
}

// #GoToolSet declares a set of tools installed via go install.
//
// Usage:
//   goTools: #GoToolSet & {
//       metadata: name: "go-tools"
//       spec: tools: {
//           gopls: {package: "golang.org/x/tools/gopls", version: "v0.21.0"}
//       }
//   }
#GoToolSet: schema.#ToolSet & {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "ToolSet"
	metadata: {
		name:        string
		description: string | *"Go tools installed via go install"
	}
	spec: {
		runtimeRef: "go"
		tools: {[string]: {
			package: string & !=""
			version: string & !=""
		}}
	}
}
