package toto

// InstallerRepository test: delegation pattern with helm
// Dependency chain: Tool/helm (aqua, latest) → Installer/helm (delegation) → InstallerRepository/bitnami

// Helm tool installed via aqua registry (latest version)
helmTool: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "helm"
	spec: {
		installerRef: "aqua"
		package:      "helm/helm"
	}
}

// Helm installer: delegation pattern using helm binary
// Used by InstallerRepository to add/manage helm repositories
helmInstaller: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "Installer"
	metadata: name: "helm"
	spec: {
		type:    "delegation"
		toolRef: "helm"
		commands: {
			install: "helm repo add {{.Name}} {{.URL}}"
		}
	}
}

// Bitnami helm repository managed via delegation
bitnamiRepo: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "InstallerRepository"
	metadata: name: "bitnami"
	spec: {
		installerRef: "helm"
		source: {
			type: "delegation"
			url:  "https://charts.bitnami.com/bitnami"
			commands: {
				install: "helm repo add bitnami https://charts.bitnami.com/bitnami"
				check:   "helm repo list 2>/dev/null | grep -q ^bitnami"
				remove:  "helm repo remove bitnami"
			}
		}
	}
}
