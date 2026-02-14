package tomei

// kubectl plugins installed via krew
krewPlugins: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "ToolSet"
	metadata: {
		name:        "krew-plugins"
		description: "kubectl plugins installed via krew"
	}
	spec: {
		installerRef: "krew"
		tools: {
			ctx: {package: "ctx"}
			ns: {package: "ns"}
			neat: {package: "neat"}
			"node-shell": {package: "node-shell"}
		}
	}
}
