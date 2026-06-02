@if(linux)

package tomei

// Phase 4 demo: SystemPackageSet via apt (Debian/Ubuntu only). Requires
// `tomei apply --system`. The file is gated with `@if(linux)` so non-Linux
// platforms ignore it cleanly, but the actual install runs through the
// built-in APT backend (internal/installer/apt) — which only works on
// apt-based distros. Fedora/Arch/Alpine users would need a different
// SystemInstaller name and a matching backend.
//
// NOTE: SystemInstaller is currently a named declaration that the
// validator binds to a built-in backend (here "apt"). The spec.commands
// strings below are descriptive — they are not what gets executed; the
// backend owns the actual invocation. No preset exists for apt yet,
// so this stanza is the canonical inline form.
apt: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "SystemInstaller"
	metadata: name: "apt"
	spec: {
		pattern:    "delegation"
		privileged: true
		commands: {
			install: {command: "sudo apt-get install -y"}
			remove: {command: "sudo apt-get remove -y"}
			check: {command: "dpkg -s"}
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
