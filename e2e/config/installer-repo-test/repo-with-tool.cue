package toto

// Full dependency chain test:
// Tool/helm (aqua, latest) → Installer/helm (delegation) → InstallerRepository/bitnami → Tool/common-chart
//
// Note: Installer/helm commands differ from helm-repo.cue.
// Here it uses "helm pull" to install charts, while helm-repo.cue uses "helm repo add".
// InstallerRepository/bitnami handles "helm repo add" via its own commands.

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

// Helm installer: delegation pattern for pulling charts
// {{.Package}} = spec.package (chart name), {{.Version}} = spec.version
helmInstaller: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "Installer"
	metadata: name: "helm"
	spec: {
		type:    "delegation"
		toolRef: "helm"
		commands: {
			install: "mkdir -p /tmp/toto-e2e-charts && helm pull bitnami/{{.Package}} --destination /tmp/toto-e2e-charts"
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

// Common chart: bitnami/common (helper-only chart, very small)
// Installed via helm pull after bitnami repository is added
commonChart: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "common-chart"
	spec: {
		installerRef:  "helm"
		repositoryRef: "bitnami"
		package:       "common"
	}
}
