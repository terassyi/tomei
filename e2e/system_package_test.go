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
// ("cowsay", "sl", "tree") and refuses anything riskier even if that
// rejects names production would happily accept. Compiled once at
// package init to avoid re-parsing per call.
var systemPackageTestPkgNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9+\-.]*$`)

// systemPackageTestDirRE is a path allowlist for writeSystemPackageManifest's
// dir argument. The helper documents a `/tmp`-only safety boundary and
// uses Sprintf-into-shell-heredoc semantics, so the regex must enforce
// BOTH constraints: the path must live under /tmp/, AND the segment after
// `/tmp/` must not contain path-traversal components or shell
// metacharacters. The earlier `^/[A-Za-z0-9._/\-]+$` allowed any absolute
// path (e.g. `/etc/tomei`) and admitted `..` via the dot character — a
// caller passing `/tmp/../../etc/tomei` would escape the scratch area.
// This tighter form pins the prefix to `/tmp/`, forbids consecutive dots
// (`\.\.`), and forbids embedded slashes inside the segment so a single
// flat `/tmp/<name>` is the only shape that passes.
var systemPackageTestDirRE = regexp.MustCompile(`^/tmp/[A-Za-z0-9_\-]+(\.[A-Za-z0-9_\-]+)*/?$`)

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
		// Defense-in-depth in case pipefail is somehow disabled by a
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
		// Toolchain probe: "Linux" alone is not enough — the specs
		// below invoke `apt-get install/remove` and `dpkg-query`, which
		// only exist on Debian-family distros. A TOMEI_E2E_NATIVE=true
		// run on Fedora/Arch/Alpine/etc. would otherwise hit
		// command-not-found mid-spec and surface as a misleading test
		// failure. We probe via `command -v` (POSIX-portable, no
		// dependency on bash specifics) so the skip message is precise
		// about which tool is missing.
		_, errApt := testExec.ExecBash("command -v apt-get >/dev/null 2>&1")
		_, errDpkg := testExec.ExecBash("command -v dpkg-query >/dev/null 2>&1")
		if errApt != nil || errDpkg != nil {
			Skip("real-apply SystemPackageSet requires apt-get and dpkg-query (Debian-family); not available on this Linux host")
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

	// fixturePackages enumerates every package this Context's BeforeAll
	// may install on the host. Centralized so the BeforeAll remove-first
	// step, the outer AfterAll cleanup, and any future drift detector
	// stay in lockstep — adding a fourth package in the manifest without
	// touching this list would silently leak host state.
	fixturePackages := []string{"cowsay", "sl", "tree"}

	// preflightComplete is set to true at the very end of Context A's
	// BeforeAll, after the remove-first preflight has succeeded and
	// `apt-get update` has run. It does NOT mean tomei has installed
	// anything yet — the first It block is what drives `apply --system`
	// — but it DOES mean Context A's preflight removed any
	// pre-existing fixture package from the host, so the outer AfterAll
	// is free to apt-get remove the same set without risk of clobbering
	// state that was on the host before this Context ran.
	//
	// Outer AfterAll consults it: if BeforeAll aborted before the
	// preflight completed (e.g. apt-get update failed, or the
	// remove-first step left a package installed), the flag stays false
	// and cleanup is a no-op so the user's pre-existing packages are
	// preserved exactly as they were.
	var preflightComplete bool

	// writeSystemPackageManifest writes a minimal CUE manifest under dir:
	// SystemInstaller/apt + SystemPackage/tree sugar + SystemPackageSet
	// cli-tools with the given package list. The single-quoted heredoc
	// terminator (<<'EOF') prevents any shell expansion inside the
	// manifest body, so the embedded `$` and `${...}` (none today, but
	// reserved for future CUE template syntax) are inert.
	//
	// pkgs is rendered as a CUE list literal; callers MUST pass safe
	// values (the only call sites are static literals "cowsay"/"sl",
	// with "tree" carried by the SystemPackage sugar resource). We
	// validate each entry against a conservative regex so a future
	// caller with dynamic input cannot break the heredoc terminator or
	// inject CUE syntax.
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
		// Clear the target directory before recreating cue.mod /
		// manifest.cue. The CUE loader reads EVERY *.cue file in the
		// directory; a stale manifest left by a failed previous run
		// (or by a developer iterating on the helper) would be loaded
		// alongside the fresh one and silently shift these assertions.
		// `rm -rf` of the whole dir is safe here because the regex
		// allowlist above restricts dir to a flat single-segment
		// /tmp/<name> path — no risk of clobbering anything outside
		// the scratch area.
		script := fmt.Sprintf(`rm -rf %[1]s && mkdir -p %[1]s/cue.mod && cat > %[1]s/cue.mod/module.cue <<'EOF'
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
			// Emergency escape hatch for developers who opt in to
			// native mode but want host state preserved for inspection
			// after a failed run.
			if os.Getenv("TOMEI_E2E_NATIVE_SKIP_CLEANUP") == "true" {
				fmt.Fprintln(GinkgoWriter, "TOMEI_E2E_NATIVE_SKIP_CLEANUP=true: skipping host package + /tmp cleanup")
				return
			}
			// Preflight gate: only apt-get remove the fixture packages
			// if Context A's BeforeAll preflight succeeded. The
			// preflight uninstalled any pre-existing copy, so removing
			// them again here is at worst a no-op rather than a
			// destructive uninstall of state that predates this
			// Context. If the preflight aborted, preflightComplete
			// stays false and we MUST NOT touch host packages — that
			// was the concrete risk Copilot flagged on the earlier
			// version. /tmp scratch dirs are always safe to remove
			// (regex-allowlisted single-segment paths created by this
			// suite only).
			if preflightComplete {
				// apt-get remove targets only the fixture packages, by
				// explicit name — no chance of broadening the blast
				// radius.
				_, _ = testExec.ExecBash("sudo -n apt-get remove -y " + strings.Join(fixturePackages, " ") + " 2>&1 || true")
				// autoremove is scoped to TOMEI_E2E_CONTAINER ONLY.
				// `apt-get autoremove` is a host-global operation: it
				// removes ANY package currently marked auto-removable,
				// not just transitive Depends introduced by this
				// suite. On an ephemeral CI container we own the
				// filesystem and the broader cleanup is desirable; on
				// a native runner or developer laptop (even with
				// TOMEI_E2E_NATIVE=true), it could remove unrelated
				// auto-installed packages the user expects to keep.
				// The trade-off: native mode may leave cowsay's
				// transitive Depends (e.g. libtext-charwidth-perl)
				// orphaned across suite runs. That's acceptable
				// because the ephemeral CI native runner is wiped
				// between jobs anyway, and a developer running with
				// TOMEI_E2E_NATIVE=true has opted in to host mutation
				// being best-effort rather than perfect.
				if os.Getenv("TOMEI_E2E_CONTAINER") != "" {
					_, _ = testExec.ExecBash("sudo -n apt-get autoremove -y 2>&1 || true")
				}
			}
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

				// Preflight remove-first: ensure the fixture packages are
				// NOT installed before tomei runs, so the subsequent
				// `apply --system` actually exercises the install path
				// rather than passing because the package happened to
				// already be on the runner.
				//
				// History: an earlier version of this Context used a hard
				// fail-on-preinstalled invariant ("pick a different
				// package") which broke the linux/arm64 native CI leg
				// because GitHub-hosted ubuntu-24.04 runners preinstall
				// `bc`. Switching to cowsay/sl (universe, leaf, NOT in
				// any known runner-image preinstall list) plus this
				// remove-first step makes the spec robust against future
				// preinstall drift on either side — if a future runner
				// image starts shipping cowsay or sl, the remove step
				// uninstalls it cleanly and the test still validates a
				// fresh install. We use NOT-purge here so /etc/cowsay
				// (none exist, but futureproof) is preserved; the outer
				// AfterAll handles the final removal.
				//
				// `2>&1 || true` swallows the failure for packages that
				// are not installed (apt-get remove exits non-zero) so
				// the BeforeAll proceeds; the assertNotInstalled checks
				// below are the real assertion that the host is clean.
				_, _ = testExec.ExecBash("sudo -n apt-get remove -y " + strings.Join(fixturePackages, " ") + " 2>&1 || true")
				for _, pkg := range fixturePackages {
					out, _ := testExec.Exec("dpkg-query", "-W", "-f=${Status}\n", "--", pkg)
					Expect(out).NotTo(ContainSubstring("install ok installed"),
						"preflight failed: %s is still installed after apt-get remove (dpkg-query Status: %q) — the apply spec below would not be exercising a real install", pkg, out)
				}

				writeSystemPackageManifest("/tmp/tomei-system-package-install", []string{"cowsay", "sl"})

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

				// Mark preflight as complete: from this point the outer
				// AfterAll is allowed to run apt-get remove against the
				// fixture packages. The preflight already uninstalled any
				// pre-existing copies, so even if no spec runs to install
				// them, the AfterAll is a safe no-op rather than a
				// destructive uninstall.
				preflightComplete = true
			})

			It("apply --system installs the manifest packages on the host", func() {
				out, err := ExecApply(testExec, "--system", installCfgPath)
				Expect(err).NotTo(HaveOccurred())
				Expect(out).To(ContainSubstring("SystemPackageSet/cli-tools"))
				Expect(out).To(ContainSubstring("SystemPackageSet/tree"))
				assertInstalled("cowsay")
				assertInstalled("sl")
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
				Expect(cliTools.InstalledVersions).To(HaveKey("cowsay"),
					"systemPackages.cli-tools.installedVersions must record cowsay; got: %v", cliTools.InstalledVersions)
				Expect(cliTools.InstalledVersions).To(HaveKey("sl"),
					"systemPackages.cli-tools.installedVersions must record sl; got: %v", cliTools.InstalledVersions)
				// Sanity-check the version field too — an empty version string
				// would mean InstalledVersions is populated with empty values,
				// which is a likely regression mode if PackageVersion() ever
				// silently fails. Non-empty proves we recorded a real version.
				Expect(cliTools.InstalledVersions["cowsay"]).NotTo(BeEmpty(),
					"cli-tools.installedVersions.cowsay must be a non-empty version string")
				Expect(cliTools.InstalledVersions["sl"]).NotTo(BeEmpty(),
					"cli-tools.installedVersions.sl must be a non-empty version string")

				tree, ok := parsed.SystemPackages["tree"]
				Expect(ok).To(BeTrue(),
					"systemPackages.tree (sugar) must exist after --system apply")
				Expect(tree.InstalledVersions).To(HaveKey("tree"),
					"systemPackages.tree.installedVersions must record the desugared package; got: %v", tree.InstalledVersions)
				// Parity with the SystemPackageSet assertions above — an
				// empty version string here would indicate PackageVersion()
				// returned "" on the desugared path even though dpkg state
				// was fine, masking a sugar-specific regression.
				Expect(tree.InstalledVersions["tree"]).NotTo(BeEmpty(),
					"tree.installedVersions.tree must be a non-empty version string (sugar path)")
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
				assertInstalled("cowsay")
				assertInstalled("sl")
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
				// Context A succeeded, cowsay/sl/tree are installed and the
				// system state.json holds entries for cli-tools/tree. Without
				// this guard, a silent failure upstream would let Context B's
				// "WITH --system runs apt-get remove" spec pass trivially
				// (nothing to remove → state already matches removal target).
				assertInstalled("cowsay")
				assertInstalled("sl")
				assertInstalled("tree")

				// Reduced manifest under /tmp/: cli-tools shrinks from
				// {cowsay, sl} to {cowsay}, so the removal driven by
				// `apply --system` is exactly the sl package. We
				// generate this as a sibling to the canonical fixture so
				// prior Contexts (validate / plan / Apply A) that read
				// ~/system-package-test/manifest.cue keep seeing a
				// byte-stable manifest.
				writeSystemPackageManifest("/tmp/tomei-system-package-removal", []string{"cowsay"})
			})

			// Host cleanup is registered at the outer Context scope (see
			// the parent Context's AfterAll). Keeping it there means a
			// Context A failure that prevents Context B from starting still
			// triggers cleanup — an inner AfterAll here would not run in
			// that case under Ginkgo's Ordered semantics.

			// parseStateInstalled is a Context-B-local helper that parses
			// state.json into the same struct shape Context A uses for
			// installedVersions, so the removal-state assertions can
			// inspect map membership instead of falling back to substring
			// matches (which were the trap Copilot flagged on the install
			// path — package names in the resource spec's `packages`
			// array can satisfy a substring check even when
			// installedVersions is empty).
			parseStateInstalled := func() map[string]map[string]string {
				raw, err := testExec.ExecBash("cat ~/.local/share/tomei/system/state.json")
				Expect(err).NotTo(HaveOccurred())
				var parsed struct {
					SystemPackages map[string]struct {
						InstalledVersions map[string]string `json:"installedVersions"`
					} `json:"systemPackages"`
				}
				Expect(json.Unmarshal([]byte(raw), &parsed)).To(Succeed(),
					"state.json must be valid JSON; got:\n%s", raw)
				out := map[string]map[string]string{}
				for name, sp := range parsed.SystemPackages {
					out[name] = sp.InstalledVersions
				}
				return out
			}

			It("apply removal manifest WITHOUT --system retains sl in state (no-op)", func() {
				hashBefore := stateHash()
				_, err := ExecApply(testExec, removalCfgPath)
				Expect(err).NotTo(HaveOccurred())
				installed := parseStateInstalled()
				// Without --system, cli-tools.installedVersions must
				// still record sl from Context A's install — system
				// resources are skip-downgraded and the state shrink
				// MUST NOT have happened yet.
				Expect(installed).To(HaveKey("cli-tools"))
				Expect(installed["cli-tools"]).To(HaveKey("sl"),
					"sl must still be recorded under cli-tools.installedVersions before --system removal; got: %v", installed["cli-tools"])
				assertInstalled("sl")
				Expect(stateHash()).To(Equal(hashBefore),
					"removal manifest applied without --system must not rewrite state.json")
			})

			It("apply removal manifest WITH --system runs apt-get remove and updates state.json", func() {
				out, err := ExecApply(testExec, "--system", removalCfgPath)
				Expect(err).NotTo(HaveOccurred())
				Expect(out).To(ContainSubstring("SystemPackageSet/cli-tools"))
				assertNotInstalled("sl")
				assertInstalled("cowsay")
				assertInstalled("tree")
				installed := parseStateInstalled()
				Expect(installed).To(HaveKey("cli-tools"),
					"cli-tools must remain in systemPackages after shrink; got keys: %v", installed)
				Expect(installed["cli-tools"]).To(HaveKey("cowsay"),
					"cowsay must remain under cli-tools.installedVersions; got: %v", installed["cli-tools"])
				Expect(installed["cli-tools"]["cowsay"]).NotTo(BeEmpty(),
					"cli-tools.installedVersions.cowsay must keep its non-empty version across the shrink")
				Expect(installed["cli-tools"]).NotTo(HaveKey("sl"),
					"sl must be removed from cli-tools.installedVersions after --system removal; got: %v", installed["cli-tools"])
				// The SystemPackage sugar entry must survive a sibling
				// set's shrink — confirms removal scope is per-resource,
				// not global.
				Expect(installed).To(HaveKey("tree"),
					"sugar entry tree must survive a sibling set's shrink; got keys: %v", installed)
				Expect(installed["tree"]).To(HaveKey("tree"))
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
