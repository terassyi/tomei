//go:build e2e

package e2e

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// pgdgSuite is the second half of the rotation contract: the suite name
// used in the manifest must stay in lockstep with this Go const. PGDG ships
// distributions under the `<codename>-pgdg` convention; using a bare
// `<codename>` would 404 at apt-get-update time. Pinned here because the
// tree-printer plan output does not currently render AptSource fields, so
// only this drift detector catches a regression of the manifest's suite
// back to a bare codename.
const pgdgSuite = "noble-pgdg"

// pgdgUbuntuRelease is the Ubuntu release that the e2e container image
// (e2e/containers/ubuntu/Dockerfile) bases on. The PGDG suite encodes the
// release's *codename*, while the Dockerfile encodes the *version number*
// — both halves must agree or `apt-get update` 404s at apply time. The
// drift detector below enforces this third leg of the rotation contract
// at test time so a Dockerfile bump without a suite bump (or vice versa)
// cannot pass CI silently.
const pgdgUbuntuRelease = "24.04"

// ubuntuReleaseToCodename pins the version-number ↔ codename mapping for
// the Ubuntu LTS releases tomei targets. The drift detector below uses
// this to close the rotation triangle:
//
//	pgdgUbuntuRelease  ── version-number leg (Dockerfile FROM)
//	         │
//	         ▼  ubuntuReleaseToCodename[pgdgUbuntuRelease]
//	      codename  ── codename leg
//	         │
//	         ▼  pgdgSuite = "<codename>-pgdg"
//	     pgdgSuite  ── PGDG suite leg
//
// Without this map, an editor could update the Dockerfile and
// pgdgUbuntuRelease together while leaving pgdgSuite pointing at the old
// codename, and every individual pair-wise drift check would still pass.
// New Ubuntu LTS releases land here in lockstep with the Dockerfile bump.
var ubuntuReleaseToCodename = map[string]string{
	"22.04": "jammy",
	"24.04": "noble",
}

// pgdgKeyHashSHA256 is the pinned SHA256 of the PostgreSQL APT signing
// key referenced from e2e/config/system-package-test/manifest.cue. The
// value is derived from
//
//	curl -fsSL https://apt.postgresql.org/pub/repos/apt/ACCC4CF8.asc \
//	  | sha256sum
//
// and must stay in lockstep with spec.apt.keyHash in that manifest. If
// upstream rotates the ACCC4CF8 signing key the manifest and this const
// MUST be updated together — the production AptSource validation refuses
// to install without a sha256:<64hex> KeyHash, so a missing or stale pin
// breaks `tomei apply --system` rather than silently trusting an
// unverified key.
const pgdgKeyHashSHA256 = "sha256:0144068502a1eddd2a0280ede10ef607d1ec592ce819940991203941564e8e76"

func systemPackageTests() {
	const cfgPath = "~/system-package-test/"

	It("validates SystemPackage and SystemPackageSet manifests", func() {
		out, err := testExec.Exec("tomei", "validate", cfgPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("SystemInstaller/apt"))
		Expect(out).To(ContainSubstring("SystemPackageSet/tree"))
		Expect(out).To(ContainSubstring("SystemPackageSet/cli-tools"))
		// Desugar contract: pre-expand SystemPackage names must not surface
		// in user-visible output. ExpandSets rewrites SystemPackage into
		// SystemPackageSet before any kind/name printing.
		Expect(out).NotTo(ContainSubstring("SystemPackage/tree"))
		Expect(out).To(ContainSubstring("Validation successful"))
	})

	It("plan without --system shows system resources (skipped)", func() {
		// Without --system, system resources are forced to ActionSkip
		// regardless of installer wiring. Action label is not asserted.
		out, err := testExec.Exec("tomei", "plan", cfgPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("SystemPackageSet/tree"))
		Expect(out).To(ContainSubstring("SystemPackageSet/cli-tools"))
	})

	It("plan --system shows expanded SystemPackageSet entries", func() {
		// With --system, SystemPackageSet currently shows "skip" because
		// the concrete APT installer is not yet wired (skipPackageInstaller
		// stub). Once #199 lands, the action label becomes "install"; the
		// loose ContainSubstring assertions stay green across the transition.
		out, err := testExec.Exec("tomei", "plan", "--system", cfgPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("SystemInstaller/apt"))
		Expect(out).To(ContainSubstring("SystemPackageSet/tree"))
		Expect(out).To(ContainSubstring("SystemPackageSet/cli-tools"))
	})

	// SystemPackageRepository coverage (#197). Mirrors the validate/plan
	// surface above for SystemInstaller / SystemPackageSet but anchors the
	// post-#196 contract that SystemPackageRepository is NOT skip-downgraded
	// the way SystemPackageSet still is: with --system the reconciler-
	// determined action passes through to the printer, so plan shows
	// "[+ install]" for an uninstalled pgdg.
	//
	// The apply / removal / idempotency arms of the spec are declared as
	// Ginkgo Pending specs (PIt) rather than running real apply against
	// the host. Rationale:
	//
	//   - The e2e runner image (e2e/containers/ubuntu/Dockerfile) does
	//     not currently install gnupg, so apt.PackageRepositoryInstaller's
	//     `gpg --dearmor` step would fail. Adding gnupg is out of scope
	//     for this PR; #197 follow-up will land the Dockerfile bump plus
	//     promote these PIt specs to It with real assertions.
	//   - Even with gnupg present, apply --system mutates host state
	//     (/usr/share/keyrings/pgdg.gpg and /etc/apt/sources.list.d/pgdg.list)
	//     which would persist across containerised tests if the runner is
	//     reused, and would reach apt.postgresql.org over the network at
	//     test time — a brittle dependency for CI. The PIt bodies
	//     document the expected post-apply invariants so the follow-up
	//     PR has a single place to flip PIt -> It.
	Context("SystemPackageRepository (#197)", func() {
		It("validates the manifest", func() {
			out, err := testExec.Exec("tomei", "validate", cfgPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(ContainSubstring("Validation successful"))
			Expect(out).To(ContainSubstring("SystemPackageRepository/pgdg"))
		})

		It("manifest pins match the Go consts (drift detector)", func() {
			// Defends against the contract documented in the manifest.cue
			// header comment and the Go const docstrings: rotating the
			// upstream PGDG signing key — or changing the PGDG suite
			// naming convention — requires updating BOTH the manifest and
			// the corresponding Go const in the same commit. A drift here
			// would silently break `tomei apply --system` once the apply
			// specs are unblocked (the suite drift is especially insidious
			// because `tomei plan` does not render AptSource fields, so
			// no plan-time assertion would catch the regression).
			//
			// Paths are anchored via e2eConfigPath (runtime.Caller in
			// executor.go) so the detector works under `go test`,
			// `go test -c`, and `dlv test` regardless of cwd.
			//
			// Assertions pin the exact CUE field syntax rather than bare
			// substring so a comment block that happens to mention the
			// value, or a near-miss like `suite: "noble-pgdg-staging"`,
			// cannot pass silently.
			raw, err := os.ReadFile(e2eConfigPath("system-package-test", "manifest.cue"))
			Expect(err).NotTo(HaveOccurred(), "manifest.cue must exist at the canonical e2e fixture path")
			Expect(string(raw)).To(ContainSubstring(`keyHash: "`+pgdgKeyHashSHA256+`"`),
				"manifest.cue spec.apt.keyHash and the Go const pgdgKeyHashSHA256 must stay in lockstep — see the manifest header comment for the rotation procedure")
			Expect(string(raw)).To(ContainSubstring(`suite:   "`+pgdgSuite+`"`),
				"manifest.cue spec.apt.suite and the Go const pgdgSuite must stay in lockstep — PGDG uses the <codename>-pgdg convention, a bare codename would 404 at apt-get update")
		})

		It("Dockerfile Ubuntu release matches pgdgUbuntuRelease and pgdgSuite codename (rotation contract)", func() {
			// Third leg of the rotation contract. The PGDG suite encodes
			// the Ubuntu codename ("noble-pgdg"); the Dockerfile encodes
			// the version number ("24.04"). The three pins must agree
			// pairwise AND transitively or `apt-get update` 404s at apply
			// time — neither the manifest pin nor the suite pin would
			// catch a transitive break on its own.
			dockerfilePath := e2eConfigPath("..", "containers", "ubuntu", "Dockerfile")
			raw, err := os.ReadFile(dockerfilePath)
			Expect(err).NotTo(HaveOccurred(), "Dockerfile must exist at the canonical e2e path")
			Expect(string(raw)).To(ContainSubstring("FROM ubuntu:"+pgdgUbuntuRelease),
				"e2e/containers/ubuntu/Dockerfile FROM line and the Go const pgdgUbuntuRelease must stay in lockstep — a Dockerfile bump without a pgdgUbuntuRelease bump would 404 at apt-get update")

			// Transitive closure: pgdgUbuntuRelease must map to a known
			// codename, and that codename must be the prefix of pgdgSuite.
			// Without this, an editor could update the Dockerfile and
			// pgdgUbuntuRelease together while leaving pgdgSuite stale
			// (the pair-wise Dockerfile↔release check would still pass).
			codename, known := ubuntuReleaseToCodename[pgdgUbuntuRelease]
			Expect(known).To(BeTrue(),
				"pgdgUbuntuRelease %q has no entry in ubuntuReleaseToCodename — add the version-number ↔ codename mapping in lockstep with any Dockerfile bump",
				pgdgUbuntuRelease)
			Expect(pgdgSuite).To(Equal(codename+"-pgdg"),
				"pgdgSuite must follow the PGDG <codename>-pgdg convention for the Ubuntu release pinned by pgdgUbuntuRelease — bump pgdgSuite to %q to match",
				codename+"-pgdg")
		})

		It("plan without --system shows the repository as skipped", func() {
			// Without --system every system resource — including
			// SystemPackageRepository, which post-#196 has a concrete
			// installer — is forced to ActionSkip by buildResourceInfo.
			//
			// MatchRegexp pins the action label to the pgdg LINE so
			// the assertion is meaningful even when other system
			// resources appear in the same output with different
			// actions. A loose ContainSubstring would still pass if
			// SystemPackageRepository/pgdg were rendered with the
			// wrong action and a sibling SystemPackageSet happened to
			// be marked skip.
			out, err := testExec.Exec("tomei", "plan", cfgPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(MatchRegexp(`SystemPackageRepository/pgdg[^\n]*\[⊘ skip\]`),
				"plan output must show SystemPackageRepository/pgdg with the [⊘ skip] marker on the same line; got:\n%s", out)
		})

		It("plan --system shows install action for the repository", func() {
			// Post-#196 contract: SystemPackageRepository is NOT
			// skip-downgraded by addSystemResourceInfo. With --system and
			// no prior state, the reconciler emits ActionInstall and the
			// tree printer renders "[+ install]" on the pgdg line. This
			// is the assertion that #214 (installer wiring) actually
			// reaches the plan path.
			//
			// MatchRegexp pins [+ install] to the pgdg LINE specifically.
			// On a first-run --system plan SystemInstaller/apt is also
			// marked install, so a loose ContainSubstring would still
			// pass even if the repository were silently downgraded.
			out, err := testExec.Exec("tomei", "plan", "--system", cfgPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(MatchRegexp(`SystemPackageRepository/pgdg[^\n]*\[\+ install\]`),
				"plan output must show SystemPackageRepository/pgdg with the [+ install] marker on the same line; got:\n%s", out)
		})

		// The three apply / idempotency / removal arms are PENDING rather
		// than Skip()'d-with-a-message. Ginkgo's PIt reports the spec as
		// "P" (pending, distinct from "S" skipped) in the suite summary,
		// preserves the Label() for `ginkgo --label-filter` opt-in, and
		// — crucially — distinguishes a stub-pending-implementation from
		// an environment-gated Skip (a Skip() reading "needs gnupg" would
		// silently disappear if the next contributor adds gnupg but
		// forgets to flip the spec, while a PIt body remains a no-op
		// until promoted to It). The follow-up PR will flip PIt -> It
		// once gnupg lands in the runner image and the assertions are
		// wired against real host state.
		PIt("apply --system installs the repository on the host",
			Label("needs-gnupg", "needs-network", "pending-197-followup"),
			func() {
				// Pending follow-up. When flipped to It, this spec must:
				//   - assert /usr/share/keyrings/pgdg.gpg is present with
				//     mode 0644 root:root,
				//   - assert /etc/apt/sources.list.d/pgdg.list is present
				//     and contains signed-by=/usr/share/keyrings/pgdg.gpg,
				//     the spec URL, and "noble-pgdg main",
				//   - assert ~/.local/share/tomei/system/state.json contains
				//     a systemPackageRepositories.pgdg entry pinning
				//     pgdgKeyHashSHA256.
				_ = pgdgKeyHashSHA256
			})

		PIt("apply --system twice is idempotent",
			Label("needs-gnupg", "needs-network", "pending-197-followup"),
			func() {
				// Pending follow-up. When flipped to It, run apply twice
				// and assert the second `tomei plan --system` between runs
				// shows no changes for SystemPackageRepository/pgdg.
			})

		PIt("removing the repo from the manifest cleans up files and state",
			Label("needs-gnupg", "needs-network", "pending-197-followup"),
			func() {
				// Pending follow-up. When flipped to It, write a sibling
				// manifest without pgdgRepo, re-apply, and assert that
				//   - /usr/share/keyrings/pgdg.gpg is gone,
				//   - /etc/apt/sources.list.d/pgdg.list is gone, and
				//   - state.json no longer mentions pgdg.
			})
	})
}
