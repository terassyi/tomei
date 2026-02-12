package tomei

// ToolSet: common CLI tools installed via aqua registry
cliTools: #AquaToolSet & {
	metadata: {
		name:        "cli-tools"
		description: "Common CLI tools for daily development"
	}
	spec: tools: {
		rg:  {package: "BurntSushi/ripgrep", version: "15.1.0"}
		fd:  {package: "sharkdp/fd", version: "v10.3.0"}
		bat: {package: "sharkdp/bat", version: "v0.26.1"}
		jq:  {package: "jqlang/jq", version: "jq-1.8.1"}
		yq:  {package: "mikefarah/yq", version: "v4.46.0"}
		fzf: {package: "junegunn/fzf", version: "v0.62.0"}
		ghq: {package: "x-motemen/ghq", version: "v1.8.0"}
		gh:  {package: "cli/cli", version: "v2.72.0"}
	}
}
