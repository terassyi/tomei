package tomei

// #AquaTool declares a single tool installed via aqua registry.
//
// Usage:
//   rg: #AquaTool & {
//       metadata: name: "rg"
//       spec: {
//           package: "BurntSushi/ripgrep"
//           version: "15.1.0"
//       }
//   }
#AquaTool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: {
		name:         string
		description?: string
	}
	spec: {
		installerRef: "aqua"
		package:      string & !=""
		version:      string & !=""
	}
}

// #AquaToolSet declares a set of tools installed via aqua registry.
//
// Usage:
//   cliTools: #AquaToolSet & {
//       metadata: name: "cli-tools"
//       spec: tools: {
//           rg:  {package: "BurntSushi/ripgrep", version: "15.1.0"}
//           fd:  {package: "sharkdp/fd", version: "v10.3.0"}
//       }
//   }
#AquaToolSet: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "ToolSet"
	metadata: {
		name:        string
		description: string | *"Tools installed via aqua registry"
	}
	spec: {
		installerRef: "aqua"
		tools: {[string]: {
			package: string & !=""
			version: string & !=""
		}}
	}
}
