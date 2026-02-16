package tomei

// Invalid installer test: cannot specify both runtimeRef and toolRef
invalidInstaller: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Installer"
	metadata: name: "invalid"
	spec: {
		type:       "delegation"
		runtimeRef: "go"
		toolRef:    "some-tool"
		commands: {install: ["echo install"]}
	}
}
