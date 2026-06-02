@if(linux)

package tomei

// Phase 4 demo: SystemPackageSet via apt (Linux only). Requires
// `tomei apply --system`. The apt SystemInstaller is declared inline —
// no preset exists for it yet, so this stanza is the canonical form.
apt: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "SystemInstaller"
	metadata: name: "apt"
	spec: {
		pattern:    "delegation"
		privileged: true
		commands: {
			install: {command: "sudo apt-get install -y"}
			remove: {command:  "sudo apt-get remove -y"}
			check: {command:   "dpkg -s"}
		}
	}
}

// build-essential is pre-installed in the example container (see
// examples/Dockerfile), so we leave it out and pick three packages
// that actually install fresh — making the demo visually verifiable
// (`tree --version` works after --system apply).
buildDeps: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "SystemPackageSet"
	metadata: name: "build-deps"
	spec: {
		installerRef: "apt"
		packages: ["pkg-config", "libssl-dev", "tree"]
	}
}
