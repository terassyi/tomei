package tomei

failInstaller: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Installer"
	metadata: name: "fail-installer"
	spec: {
		type: "delegation"
		commands: {
			install: "echo 'step 1: preparing...' && echo 'step 2: downloading...' && echo 'step 3: ERROR: build failed' && exit 1"
			remove:  "echo removing"
		}
	}
}

failTool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "fail-tool"
	spec: {
		installerRef: "fail-installer"
		package:      "fake-package"
		version:      "1.0.0"
	}
}
