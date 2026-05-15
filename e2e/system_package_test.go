//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strings"

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

// systemPackageTestPkgNameRE is a conservative allowlist for package
// names appearing in the writeSystemPackageManifest helper. It rejects
// any name that could break the single-quoted heredoc body or inject CUE
// syntax (quotes, whitespace, control chars, slashes). Production
// validation in internal/installer/apt/apt.go uses a blacklist of
// disallowed characters which accepts a wider set of Debian-legal names
// (e.g., uppercase, longer punctuation); this test-helper allowlist is
// intentionally narrower — it only needs to admit the small fixture set
// ("bc", "cowsay", "tree") and refuses anything riskier even if that
// rejects names production would happily accept. Compiled once at
// package init to avoid re-parsing per call.
var systemPackageTestPkgNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9+\-.]*$`)

// systemPackageTestDirRE is a path allowlist for writeSystemPackageManifest's
// dir argument. Callers currently pass only static literals under /tmp/,
// but Sprintf-into-shell-heredoc semantics mean any future caller passing
// a path with shell metacharacters (`$`, backticks, `;`) would break out
// of the heredoc. Defense-in-depth: refuse anything that isn't an
// alphanumeric/`/`/`-`/`_`/`.` path so the failure mode is a loud
// Expect rather than silent code execution.
var systemPackageTestDirRE = regexp.MustCompile(`^/[A-Za-z0-9._/\-]+$`)

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

	// SystemPackageSet apply coverage (#200). Mutates host packages, so
	// gated to linux (apt) AND to orchestrated e2e runs (TOMEI_E2E_CONTAINER
	// or TOMEI_E2E_NATIVE — see skipIfNotLinux). The two inner Contexts
	// below are Ordered (inherited from the suite-level `Describe(...,
	// Ordered, ...)` in suite_test.go): Context A applies a generated
	// /tmp/tomei-system-package-install/ manifest and asserts install +
	// state-record + no-op + idempotency; Context B applies a reduced
	// /tmp/tomei-system-package-removal/ manifest and asserts remove +
	// state-shrink + idempotency.
	//
	// Wrapped in an outer Context so that the host-cleanup AfterAll
	// (apt-get remove + rm -rf /tmp) lives at a scope that covers BOTH
	// inner contexts: if Context A's BeforeAll or any Context A spec
	// fails partway, an inner Context-B-scoped AfterAll would either be
	// skipped or run with a misleading host state. Outer-scope AfterAll
	// guarantees host cleanup even when Context A aborts before B starts.
	//
	// dpkg-query -W -f='${Status}' is preferred over dpkg -l because
	// `dpkg -l` exits 0 for the `rc` ("removed but config present") state
	// after `apt-get remove` without --purge — a false-positive trap for
	// post-remove assertions. `install ok installed` is the only state
	// that counts as "installed"; `deinstall ok config-files` (the
	// post-remove state) is correctly excluded by NotTo ContainSubstring.

	// dpkg-query is invoked via argv form (testExec.Exec) rather than
	// shell-string concatenation: the package name lives in its own argv
	// slot, eliminating the shell-injection footgun even if a future
	// caller passes a dynamic pkg string. `--` defends against pkg names
	// that could look like options. dpkg-query exits non-zero when no
	// matching package is in the dpkg DB (the post-purge state); for
	// assertInstalled this is a failure, for assertNotInstalled it counts
	// as "removed".
	assertInstalled := func(pkg string) {
		out, err := testExec.Exec("dpkg-query", "-W", "-f=${Status}\n", "--", pkg)
		Expect(err).NotTo(HaveOccurred(), "dpkg-query failed for %s: %s", pkg, out)
		Expect(strings.TrimSpace(out)).To(Equal("install ok installed"),
			"package %s should be installed; dpkg-query Status: %q", pkg, out)
	}
	assertNotInstalled := func(pkg string) {
		// apt-get remove (NOT --purge) leaves dpkg in
		// "deinstall ok config-files"; a never-installed or purged package
		// produces an exit-1 with empty stdout from dpkg-query. Both count
		// as "removed"; only "install ok installed" is forbidden.
		out, _ := testExec.Exec("dpkg-query", "-W", "-f=${Status}\n", "--", pkg)
		Expect(out).NotTo(ContainSubstring("install ok installed"),
			"package %s must not be in installed state; dpkg-query: %q", pkg, out)
	}

	// stateHash returns sha256 of the system state.json. Used over mtime
	// equality because ext4 mtime granularity is 1 second — a touch within
	// the same second as the prior write would silently pass an mtime
	// check. sha256sum exits non-zero if the file is missing, which the
	// Expect surfaces as a clear failure.
	stateHash := func() string {
		// `set -o pipefail` is required: without it, the pipeline's exit
		// status is awk's (almost always 0), so a missing or unreadable
		// state.json would return awk's stderr instead of a hash and the
		// downstream byte-stability assertions would pass for the wrong
		// reason. With pipefail, sha256sum's non-zero exit propagates and
		// the Expect surfaces the real failure.
		out, err := testExec.ExecBash("set -o pipefail; sha256sum ~/.local/share/tomei/system/state.json | awk '{print $1}'")
		Expect(err).NotTo(HaveOccurred(), "sha256sum on system state.json failed: %s", out)
		hash := strings.TrimSpace(out)
		// Defence-in-depth in case pipefail is somehow disabled by a
		// future shell wrapper change: a real sha256sum hash is exactly
		// 64 lowercase hex chars. Anything else means we got stderr,
		// "No such file", an awk error, or an empty pipe — fail loudly
		// instead of silently returning a non-hash that would compare
		// equal across two equally-broken calls.
		Expect(hash).To(MatchRegexp(`^[0-9a-f]{64}$`),
			"stateHash did not return a sha256 hex digest; got %q (state.json may be missing or unreadable)", hash)
		return hash
	}

	skipIfNotLinux := func() {
		targetOS := runtime.GOOS
		if os.Getenv("TOMEI_E2E_CONTAINER") != "" {
			targetOS = "linux"
		}
		if targetOS != "linux" {
			Skip("real-apply SystemPackageSet requires apt; current OS is " + targetOS)
		}
		// Opt-in gate: a bare `go test ./e2e/...` on a Linux developer
		// laptop would otherwise run `sudo apt-get install/remove` and
		// mutate the dev's host packages — surprising and destructive.
		// The orchestrated e2e modes (`make test-e2e` containerised, and
		// the CI native legs) both set one of these env vars, so
		// requiring at least one is opt-in for orchestrated runs while
		// fail-safe for ad-hoc local invocations.
		if os.Getenv("TOMEI_E2E_CONTAINER") == "" && os.Getenv("TOMEI_E2E_NATIVE") == "" {
			Skip("real-apply SystemPackageSet mutates host apt state; set TOMEI_E2E_CONTAINER or TOMEI_E2E_NATIVE to opt in (both are set automatically by `make test-e2e` / CI; a bare `go test ./e2e/...` skips by design)")
		}
	}

	// installCfgPath / removalCfgPath are sibling manifests under /tmp/
	// that exercise the SystemPackageSet apply path WITHOUT pgdgRepo.
	// pgdg's installer needs `gpg --dearmor` and `gnupg` is intentionally
	// not in the runner image (#197 follow-up will add it together with a
	// network mock). The canonical fixture at ~/system-package-test/ keeps
	// pgdgRepo so the prior validate/plan/PIt Contexts continue to assert
	// against it; the apply Contexts here use their own sibling manifests
	// to isolate the SystemPackageSet apply path.
	//
	// TODO(#197-followup): once gnupg lands in the runner image and PGDG
	// network access can be mocked, re-align the install/removal heredocs
	// with the canonical fixture (add pgdgRepo) and either promote the
	// existing PIts to It or fold the SystemPackageRepository apply
	// coverage into this Context.
	const installCfgPath = "/tmp/tomei-system-package-install/"
	const removalCfgPath = "/tmp/tomei-system-package-removal/"

	// writeSystemPackageManifest writes a minimal CUE manifest under dir:
	// SystemInstaller/apt + SystemPackage/tree sugar + SystemPackageSet
	// cli-tools with the given package list. The single-quoted heredoc
	// terminator (<<'EOF') prevents any shell expansion inside the
	// manifest body, so the embedded `$` and `${...}` (none today, but
	// reserved for future CUE template syntax) are inert.
	//
	// pkgs is rendered as a CUE list literal; callers MUST pass safe
	// values (the only call sites are static literals "bc"/"cowsay"). We
	// validate each entry against a conservative regex so a future caller
	// with dynamic input cannot break the heredoc terminator or inject
	// CUE syntax.
	writeSystemPackageManifest := func(dir string, pkgs []string) {
		Expect(systemPackageTestDirRE.MatchString(dir)).To(BeTrue(),
			"dir %q does not match the absolute-path allowlist; only static /tmp paths are accepted by this helper", dir)
		for _, p := range pkgs {
			Expect(systemPackageTestPkgNameRE.MatchString(p)).To(BeTrue(),
				"pkg %q does not match the conservative test-helper allowlist; the production allowlist is in internal/installer/apt/apt.go", p)
		}
		quoted := make([]string, len(pkgs))
		for i, p := range pkgs {
			quoted[i] = `"` + p + `"`
		}
		script := fmt.Sprintf(`mkdir -p %[1]s/cue.mod && cat > %[1]s/cue.mod/module.cue <<'EOF'
module: "tomei.local@v0"
language: version: "v0.9.0"
EOF
cat > %[1]s/manifest.cue <<'EOF'
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
		packages: [%[2]s]
	}
}
EOF`, dir, strings.Join(quoted, ", "))
		_, err := testExec.ExecBash(script)
		Expect(err).NotTo(HaveOccurred(), "writing manifest for %s failed", dir)
	}

	Context("Apply --system mutates host packages (#200)", func() {
		// Outer-scope cleanup. Runs after every inner spec in both
		// Context A and Context B has completed (or been skipped),
		// regardless of which inner Context failed. Best-effort:
		// tolerate non-zero exits so a failure inside cleanup never
		// masks the spec failure that surfaced the real bug. apt-get
		// remove (NOT --purge) is enough — we don't care about
		// lingering config files on an ephemeral CI runner.
		AfterAll(func() {
			// Outer cleanup gates on the same opt-in env vars that
			// gate the inner specs (see skipIfNotLinux). Without one
			// of them, the inner specs Skip()'d and there is nothing
			// to clean up; running apt-get remove anyway would be
			// surprising and potentially destructive on a developer
			// laptop that happens to share this binary.
			if os.Getenv("TOMEI_E2E_CONTAINER") == "" && os.Getenv("TOMEI_E2E_NATIVE") == "" {
				return
			}
			// Native-mode safety: on a CI native runner the runner is
			// ephemeral and apt cleanup is safe. We still keep the
			// TOMEI_E2E_NATIVE=true short-circuit as an emergency
			// escape hatch documented for developers who explicitly
			// opted in to native-mode-with-cleanup-disabled.
			if os.Getenv("TOMEI_E2E_NATIVE_SKIP_CLEANUP") == "true" {
				fmt.Fprintln(GinkgoWriter, "TOMEI_E2E_NATIVE_SKIP_CLEANUP=true: skipping host package + /tmp cleanup")
				return
			}
			_, _ = testExec.ExecBash("sudo -n apt-get remove -y bc cowsay tree 2>&1 || true")
			_, _ = testExec.ExecBash("rm -rf /tmp/tomei-system-package-install /tmp/tomei-system-package-removal")
		})

		Context("Apply --system (real install)", func() {
			BeforeAll(func() {
				skipIfNotLinux()

				// Ensure tomei is initialized — `tomei apply` aborts early with
				// "tomei is not initialized" otherwise. --force makes the call
				// idempotent across prior Contexts (some of which may have
				// already run init). Pattern: e2e/privileged_test.go:16.
				_, _ = testExec.Exec("tomei", "init", "--yes", "--force")

				// Reset SYSTEM state only — user-store belongs to other suites.
				// Top-level JSON keys match internal/state/state.go SystemState.
				// Failure here means the home directory is misconfigured and
				// every downstream spec would surface a misleading error, so
				// fail loudly instead of swallowing the error.
				_, err := testExec.ExecBash(`mkdir -p ~/.local/share/tomei/system && echo '{"version":"1","systemInstallers":{},"systemPackageRepositories":{},"systemPackages":{}}' > ~/.local/share/tomei/system/state.json`)
				Expect(err).NotTo(HaveOccurred(), "failed to reset system state.json")

				// Defense-in-depth: if a future Dockerfile change adds any
				// fixture pkg to the preinstall list, a post-apply dpkg-query
				// check would pass even if tomei did nothing. Detect that drift
				// at BeforeAll time with a precise error message.
				for _, pkg := range []string{"bc", "cowsay", "tree"} {
					out, _ := testExec.ExecBash("dpkg-query -W -f='${Status}\\n' " + pkg + " 2>&1 || true")
					Expect(out).NotTo(ContainSubstring("install ok installed"),
						"fixture invariant violated: %s is preinstalled on the runner — pick a different package", pkg)
				}

				writeSystemPackageManifest("/tmp/tomei-system-package-install", []string{"bc", "cowsay"})

				// PackageSetInstaller.Install does not refresh the apt index
				// (PackageRepositoryInstaller does, but only that one). CI
				// images carry stale indexes; refresh once. Tolerate non-zero
				// exit — apt exits non-zero on any mirror failure, but a
				// partial refresh is usually enough for the apply step to
				// produce a clearer downstream error than `update` would.
				// The err is captured (not silently dropped) so a complete
				// mirror outage surfaces in GinkgoWriter alongside the output.
				out, err := testExec.ExecBash("sudo -n apt-get update -qq 2>&1")
				fmt.Fprintf(GinkgoWriter, "apt-get update (err=%v):\n%s\n", err, out)
			})

			It("apply --system installs the manifest packages on the host", func() {
				out, err := ExecApply(testExec, "--system", installCfgPath)
				Expect(err).NotTo(HaveOccurred())
				Expect(out).To(ContainSubstring("SystemPackageSet/cli-tools"))
				Expect(out).To(ContainSubstring("SystemPackageSet/tree"))
				assertInstalled("bc")
				assertInstalled("cowsay")
				assertInstalled("tree")
			})

			It("records InstalledVersions for the sugar and the set in state.json", func() {
				// Parse the JSON rather than substring-matching "bc" / "cowsay":
				// the package names also appear in the `packages` array of the
				// resource spec snapshot, so a substring match would pass even
				// if `installedVersions` were empty or missing — exactly the
				// regression this spec is meant to catch. Scope the assertion
				// to systemPackages[name].installedVersions and check map
				// membership so a future renaming or relocation of the field
				// surfaces as a real failure rather than a green test.
				raw, err := testExec.ExecBash("cat ~/.local/share/tomei/system/state.json")
				Expect(err).NotTo(HaveOccurred())

				// Anonymous struct mirrors only the fields under test; unknown
				// fields are ignored by encoding/json so this assertion stays
				// stable as the state schema evolves around it.
				var parsed struct {
					SystemPackages map[string]struct {
						InstalledVersions map[string]string `json:"installedVersions"`
					} `json:"systemPackages"`
				}
				Expect(json.Unmarshal([]byte(raw), &parsed)).To(Succeed(),
					"state.json must be valid JSON; got:\n%s", raw)

				cliTools, ok := parsed.SystemPackages["cli-tools"]
				Expect(ok).To(BeTrue(),
					"systemPackages.cli-tools must exist after --system apply; systemPackages contained: %v",
					parsed.SystemPackages)
				Expect(cliTools.InstalledVersions).To(HaveKey("bc"),
					"systemPackages.cli-tools.installedVersions must record bc; got: %v", cliTools.InstalledVersions)
				Expect(cliTools.InstalledVersions).To(HaveKey("cowsay"),
					"systemPackages.cli-tools.installedVersions must record cowsay; got: %v", cliTools.InstalledVersions)
				// Sanity-check the version field too — an empty version string
				// would mean InstalledVersions is populated with empty values,
				// which is a likely regression mode if PackageVersion() ever
				// silently fails. Non-empty proves we recorded a real version.
				Expect(cliTools.InstalledVersions["bc"]).NotTo(BeEmpty(),
					"cli-tools.installedVersions.bc must be a non-empty version string")
				Expect(cliTools.InstalledVersions["cowsay"]).NotTo(BeEmpty(),
					"cli-tools.installedVersions.cowsay must be a non-empty version string")

				tree, ok := parsed.SystemPackages["tree"]
				Expect(ok).To(BeTrue(),
					"systemPackages.tree (sugar) must exist after --system apply")
				Expect(tree.InstalledVersions).To(HaveKey("tree"),
					"systemPackages.tree.installedVersions must record the desugared package; got: %v", tree.InstalledVersions)
			})

			It("apply WITHOUT --system is a no-op (system resources skip-downgraded)", func() {
				// Anchor the prior install: if the previous It silently failed,
				// the state would be empty and "no rewrite" would trivially
				// pass. MatchRegexp pins the structural shape — systemPackages
				// is a non-empty map with cli-tools as a top-level key — so a
				// stray "cli-tools" substring elsewhere in the JSON (e.g. in a
				// later nested ref) can't satisfy the assertion.
				rawBefore, _ := testExec.ExecBash("cat ~/.local/share/tomei/system/state.json")
				Expect(rawBefore).To(MatchRegexp(`"systemPackages"\s*:\s*\{[^}]*"cli-tools"`),
					"prerequisite: prior --system apply must have populated systemPackages.cli-tools in state.json before testing no-op semantics; got:\n%s", rawBefore)

				hashBefore := stateHash()
				out, err := ExecApply(testExec, installCfgPath)
				Expect(err).NotTo(HaveOccurred())
				Expect(out).To(ContainSubstring("SystemPackageSet/cli-tools"))
				assertInstalled("bc")
				assertInstalled("cowsay")
				assertInstalled("tree")
				Expect(stateHash()).To(Equal(hashBefore),
					"apply without --system must not rewrite system state.json")
			})

			It("re-apply --system is idempotent (plan shows zero work)", func() {
				// Plan summary is matched by regex so harmless cosmetic
				// changes to the tree.go printer (color codes, spacing) do not
				// break the assertion; the four-counter structure is the
				// invariant. The trailing `, N disabled` segment only renders
				// when ActionSkip > 0, which is not the case here post-#216
				// (SystemPackageSet is no longer skip-downgraded with --system).
				out, err := testExec.Exec("tomei", "plan", "--system", installCfgPath)
				Expect(err).NotTo(HaveOccurred())
				Expect(out).To(MatchRegexp(`Summary:\s+0 to install,\s+0 to upgrade,\s+0 to reinstall,\s+0 to remove\b`))

				hashBefore := stateHash()
				_, err = ExecApply(testExec, "--system", installCfgPath)
				Expect(err).NotTo(HaveOccurred())
				Expect(stateHash()).To(Equal(hashBefore),
					"idempotent re-apply must not rewrite state.json")
			})
		})

		Context("Apply --system removal and idempotency", func() {
			BeforeAll(func() {
				skipIfNotLinux()

				// Pre-flight: anchor Context A's post-install state. If
				// Context A succeeded, bc/cowsay/tree are installed and the
				// system state.json holds entries for cli-tools/tree. Without
				// this guard, a silent failure upstream would let Context B's
				// "WITH --system runs apt-get remove" spec pass trivially
				// (nothing to remove → state already matches removal target).
				assertInstalled("bc")
				assertInstalled("cowsay")
				assertInstalled("tree")

				// Reduced manifest under /tmp/ — do NOT edit the canonical
				// fixture at ~/system-package-test/manifest.cue in place; the
				// prior Contexts (validate / plan / Apply A) depend on it
				// being byte-stable.
				writeSystemPackageManifest("/tmp/tomei-system-package-removal", []string{"bc"})
			})

			// Host cleanup is registered at the outer Context scope (see
			// the parent Context's AfterAll). Keeping it there means a
			// Context A failure that prevents Context B from starting still
			// triggers cleanup — an inner AfterAll here would not run in
			// that case under Ginkgo's Ordered semantics.

			It("apply removal manifest WITHOUT --system retains cowsay in state (no-op)", func() {
				hashBefore := stateHash()
				_, err := ExecApply(testExec, removalCfgPath)
				Expect(err).NotTo(HaveOccurred())
				raw, _ := testExec.ExecBash("cat ~/.local/share/tomei/system/state.json")
				Expect(raw).To(ContainSubstring(`"cowsay"`))
				assertInstalled("cowsay")
				Expect(stateHash()).To(Equal(hashBefore),
					"removal manifest applied without --system must not rewrite state.json")
			})

			It("apply removal manifest WITH --system runs apt-get remove and updates state.json", func() {
				out, err := ExecApply(testExec, "--system", removalCfgPath)
				Expect(err).NotTo(HaveOccurred())
				Expect(out).To(ContainSubstring("SystemPackageSet/cli-tools"))
				assertNotInstalled("cowsay")
				assertInstalled("bc")
				assertInstalled("tree")
				raw, _ := testExec.ExecBash("cat ~/.local/share/tomei/system/state.json")
				Expect(raw).To(ContainSubstring(`"cli-tools"`))
				Expect(raw).To(ContainSubstring(`"bc"`))
				// The SystemPackage sugar entry must survive a sibling set's
				// shrink — confirms removal scope is per-resource, not global.
				Expect(raw).To(ContainSubstring(`"tree"`))
				Expect(raw).NotTo(ContainSubstring(`"cowsay"`))
			})

			It("re-apply removal manifest WITH --system is idempotent", func() {
				// Mirrors Context A's idempotency: once the removal has
				// converged, a second --system apply against the same reduced
				// manifest must be a plan-zero / state-byte-stable no-op.
				out, err := testExec.Exec("tomei", "plan", "--system", removalCfgPath)
				Expect(err).NotTo(HaveOccurred())
				Expect(out).To(MatchRegexp(`Summary:\s+0 to install,\s+0 to upgrade,\s+0 to reinstall,\s+0 to remove\b`))

				hashBefore := stateHash()
				_, err = ExecApply(testExec, "--system", removalCfgPath)
				Expect(err).NotTo(HaveOccurred())
				Expect(stateHash()).To(Equal(hashBefore),
					"idempotent re-apply of the removal manifest must not rewrite state.json")
			})
		})
	})
}
