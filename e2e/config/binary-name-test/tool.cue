package manifests

// binaryName override test: krew installed as kubectl-krew
_binaryNameTestVersion: "v0.4.4"

krew: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "krew"
	spec: {
		installerRef: "aqua"
		version:      _binaryNameTestVersion
		package: {
			owner: "kubernetes-sigs"
			repo:  "krew"
		}
		binaryName: "kubectl-krew"
	}
}
