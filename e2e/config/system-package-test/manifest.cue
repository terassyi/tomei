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
			remove: {command: "sudo apt-get remove -y"}
			check: {command: "dpkg -s"}
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

// cliTools is the multi-package SystemPackageSet fixture used by the
// validate / plan E2E coverage in e2e/system_package_test.go.
//
// IMPORTANT — the real-apply E2E does NOT read this fixture. The
// apply Contexts generate their own /tmp manifests with
// hard-coded package lists at runtime; this file is only consumed
// by `tomei validate ~/system-package-test/` and `tomei plan
// ~/system-package-test/`. The duplication is intentional (the
// apply path needs to mutate /tmp without touching the canonical
// fixture used by other Contexts), but it means that adding a
// fourth package here does NOT automatically extend the apply
// coverage — you also need to update fixtureSetInstall /
// fixtureSetRemoval / fixtureSugarPkg in system_package_test.go.
//
// Package selection criteria (applied to BOTH this fixture and the
// apply-side list):
//
//   - leaf packages with no reverse dependencies (so a post-suite
//     apt-get remove cannot cascade into uninstalling unrelated host
//     packages on the runner);
//   - in the `universe` pocket (NOT `main`), so the GitHub-hosted
//     ubuntu-24.04 runner image — which preinstalls essentially every
//     `main` utility, including bc and other obvious candidates — does
//     NOT ship them by default;
//   - small, daemon-free, deterministic;
//   - NOT in e2e/containers/ubuntu/Dockerfile preinstall list.
//
// cowsay and sl satisfy all four. bc was the first attempt and broke
// the CI native legs (`fixture invariant violated: bc is preinstalled
// on the runner`) — see the GitHub runner image inventory at
// actions/runner-images.
cliTools: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "SystemPackageSet"
	metadata: name: "cli-tools"
	spec: {
		installerRef: "apt"
		packages: ["cowsay", "sl"]
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
