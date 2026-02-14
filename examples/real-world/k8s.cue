package tomei

import "tomei.terassyi.net/presets/aqua"

// Kubernetes CLI tools installed via aqua registry
k8sTools: aqua.#AquaToolSet & {
	metadata: {
		name:        "k8s-tools"
		description: "Kubernetes CLI tools"
	}
	spec: tools: {
		kubectl: {package: "kubernetes/kubernetes/kubectl", version: "v1.35.1"}
		kustomize: {package: "kubernetes-sigs/kustomize", version: "v5.8.1"}
		helm: {package: "helm/helm", version: "v4.1.0"}
		kind: {package: "kubernetes-sigs/kind", version: "v0.31.0"}
	}
}

// krew — kubectl plugin manager, installed via aqua
krewTool: aqua.#AquaTool & {
	metadata: name: "kubectl-krew"
	spec: {
		package: "kubernetes-sigs/krew"
		version: "v0.4.5"
	}
}

// krew Installer (delegation) — uses krew binary to install kubectl plugins
krewInstaller: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Installer"
	metadata: {
		name:        "krew"
		description: "kubectl plugin manager"
	}
	spec: {
		type:    "delegation"
		toolRef: "kubectl-krew"
		commands: {
			install: "kubectl-krew install {{.Package}}"
			remove:  "kubectl-krew uninstall {{.Package}}"
		}
	}
}
