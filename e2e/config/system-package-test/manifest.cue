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

// pgdgRepo declares the PostgreSQL APT repository (PGDG) as the canonical
// SystemPackageRepository fixture for E2E coverage of #196 / #197. The
// armored signing key at spec.apt.keyUrl is pinned via spec.apt.keyHash —
// if the upstream key rotates, the SHA256 below must be re-derived from
// `curl -fsSL https://apt.postgresql.org/pub/repos/apt/ACCC4CF8.asc |
// sha256sum` and the e2e Go const pgdgKeyHashSHA256 updated in lockstep.
//
// Validate and plan only read the manifest — they do NOT fetch keyUrl or
// hit the network. apply (currently declared as Ginkgo Pending via PIt in
// e2e/system_package_test.go, awaiting gnupg in the runner image) is the
// only path that fetches the key, verifies the hash, and reaches the
// suite. The suite below uses the PGDG-specific naming convention
// `<codename>-pgdg` (not bare `<codename>`): the PostgreSQL project ships
// distributions under e.g. `noble-pgdg`, `jammy-pgdg`, `bookworm-pgdg`.
// Using a bare suite name would 404 the moment apply lands.
pgdgRepo: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "SystemPackageRepository"
	metadata: name: "pgdg"
	spec: {
		installerRef: "apt"
		apt: {
			url:     "https://apt.postgresql.org/pub/repos/apt"
			keyUrl:  "https://apt.postgresql.org/pub/repos/apt/ACCC4CF8.asc"
			keyHash: "sha256:0144068502a1eddd2a0280ede10ef607d1ec592ce819940991203941564e8e76"
			suite:   "noble-pgdg"
			components: ["main"]
		}
	}
}
