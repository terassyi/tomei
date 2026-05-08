package tomei

apt: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "SystemInstaller"
	metadata: name: "apt"
	spec: {
		pattern:    "delegation"
		privileged: true
		commands: {
			install: {command: "sudo apt-get install -y"}
			remove:  {command: "sudo apt-get remove -y"}
			check:   {command: "dpkg -s"}
		}
	}
}

tree: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "SystemPackage"
	metadata: name: "tree"
	spec: {
		installerRef: "apt"
		package:      "tree"
	}
}

cliTools: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "SystemPackageSet"
	metadata: name: "cli-tools"
	spec: {
		installerRef: "apt"
		packages: ["jq", "curl"]
	}
}
