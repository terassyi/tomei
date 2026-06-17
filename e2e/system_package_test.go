//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/terassyi/tomei/internal/state"
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

// pgdgURL / pgdgKeyURL / pgdgComponent pin the remaining AptSource
// fields. The drift detector below asserts each against the canonical
// fixture at e2e/config/system-package-test/manifest.cue so the scratch
// /tmp manifest emitted by writeSystemPackageRepositoryManifest cannot
// silently diverge from what validate/plan exercise. Without these
// pins, the apply path could test against a different repo URL than
// the validate/plan coverage and a fixture edit would not be caught.
const (
	pgdgURL       = "https://apt.postgresql.org/pub/repos/apt"
	pgdgKeyURL    = "https://apt.postgresql.org/pub/repos/apt/ACCC4CF8.asc"
	pgdgComponent = "main"
)

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
	// The apply / removal / idempotency arms of the spec are exercised
	// against the host in the sibling Ordered Context "Apply --system
	// installs SystemPackageRepository (#218)" below (gnupg ships in the
	// e2e runner image only as the keyring-validation oracle now that apply
	// dearmors in-process, #283; see e2e/containers/ubuntu/Dockerfile).
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
			// Pin the remaining AptSource fields against the canonical
			// fixture: the apply path's scratch /tmp manifest uses
			// these Go consts directly, so without these drift checks
			// the fixture could be edited (e.g. mirror URL change) and
			// the validate/plan vs. apply coverage would silently
			// exercise different repository definitions.
			Expect(string(raw)).To(ContainSubstring(`url:     "`+pgdgURL+`"`),
				"manifest.cue spec.apt.url and the Go const pgdgURL must stay in lockstep")
			Expect(string(raw)).To(ContainSubstring(`keyUrl:  "`+pgdgKeyURL+`"`),
				"manifest.cue spec.apt.keyUrl and the Go const pgdgKeyURL must stay in lockstep")
			Expect(string(raw)).To(ContainSubstring(`components: ["`+pgdgComponent+`"]`),
				"manifest.cue spec.apt.components and the Go const pgdgComponent must stay in lockstep")
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

		// Apply / idempotency / removal / retain-without-flag specs for
		// SystemPackageRepository live in the sibling Ordered Context
		// `Apply --system installs SystemPackageRepository (#218)` below.
		// They are host-mutating (touch /usr/share/keyrings/ and
		// /etc/apt/sources.list.d/) so they gate on TOMEI_E2E_CONTAINER /
		// TOMEI_E2E_NATIVE via skipIfNotLinux, whereas the validate/plan
		// specs in this Context run unconditionally.
	})

	// SystemPackageSet apply coverage (#200). Mutates host packages, so
	// gated to linux (apt) AND to orchestrated e2e runs (TOMEI_E2E_CONTAINER
	// or TOMEI_E2E_NATIVE — see skipIfNotLinux). The two inner Contexts
	// below are Ordered (inherited from the suite-level `Describe(...,
	// Ordered, ...)` in suite_test.go): Context A applies a generated
	// /tmp/tomei-system-package-install-<pid>-<ns>/ manifest and
	// asserts install + state-record + no-op + idempotency; Context B
	// applies a reduced /tmp/tomei-system-package-removal-<pid>-<ns>/
	// manifest and asserts remove + state-shrink + idempotency. The
	// <pid>-<ns> suffix is the per-process scratch suffix described
	// in the scratchSuffix block below.
	//
	// Wrapped in an outer Context so that the host-cleanup AfterAll
	// (apt-get remove + rm -rf /tmp) lives at a scope that covers BOTH
	// inner contexts: if Context A's BeforeAll or any Context A spec
	// fails partway, an inner Context-B-scoped AfterAll would either be
	// skipped or run with a misleading host state. Outer-scope AfterAll
	// guarantees host cleanup even when Context A aborts before B starts.
	//
	// dpkg-query -W is preferred over dpkg -l because `dpkg -l` exits 0
	// for the `rc` ("removed but config present") state after `apt-get
	// remove` without --purge — a false-positive trap for post-remove
	// assertions. The helper below queries `${db:Status-Status}` (the
	// current-state sub-field) and matches `installed` exactly, so
	// `config-files` (post-remove) and the half-installed transitional
	// states cleanly fail the installed check; held-installed packages
	// (`hold ok installed` in the full Status triple) also collapse to
	// `installed` here and are correctly classified as installed.

	// pkgCurrentState below uses ExecBash (not argv-form Exec) because
	// it needs to set LC_ALL=C / LANGUAGE=C on the dpkg-query
	// invocation — the not-found stderr message is localized otherwise.
	// Shell-injection safety relies on a regex allowlist enforced
	// inside the helper itself (`systemPackageTestPkgNameRE`, the same
	// pattern used by writeSystemPackageManifest): the regex rejects
	// every shell-meaningful character so embedding pkg into the script
	// is safe. The earlier argv-form invocation lost the locale lever
	// and was replaced by the ExecBash + regex pattern documented here.
	// dpkg-query exits non-zero when no matching package is in the
	// dpkg DB (the post-purge state); for assertInstalled this is a
	// failure, for assertNotInstalled it counts as "removed".
	//
	// Format string uses `${db:Status-Status}` (the third sub-field of
	// Status — the *current* state) rather than the full `${Status}`
	// triple. The full triple is `<DesiredState> <ErrorFlag>
	// <CurrentState>` so a substring match on "install ok installed"
	// would miss `hold ok installed` (held packages, which ARE
	// installed) and misclassify them as removed. Pinning on the
	// current-state sub-field collapses the answer to a single token
	// (`installed`, `not-installed`, `config-files`, `half-installed`,
	// etc.) so the assertion is exact regardless of the desired-state
	// dimension.
	// pkgCurrentState returns the dpkg current-state for pkg ("installed",
	// "not-installed", "config-files", "half-installed", etc.) or "" when
	// the package is not in the dpkg database at all.
	//
	// testExec.Exec captures via CombinedOutput, so stderr is mixed into
	// the output buffer. When dpkg-query can't find a package (the
	// never-installed / purged case), it writes "dpkg-query: no packages
	// found matching <pkg>" to stderr and exits 1 with EMPTY stdout.
	// Returning the raw combined output here would surface that
	// diagnostic string as the "state", which is NOT in the removedStates
	// allowlist.
	//
	// CRITICAL: we MUST distinguish the documented exit-1 "no packages
	// found" case from any other dpkg-query failure (DB lock, corruption,
	// dpkg-query binary missing, etc.). Earlier code collapsed every
	// err != nil to "" — which meant a real dpkg failure looked
	// identical to "package absent" and would silently let a pre-test
	// snapshot record the wrong state OR let assertNotInstalled pass for
	// the wrong reason. We now match the specific stderr message format
	// dpkg-query uses for the not-found case and Fail() on anything else.
	pkgCurrentState := func(pkg string) string {
		// pkg is vetted against systemPackageTestPkgNameRE before
		// reaching this helper: callers come from fixturePackages
		// (static literals) and from the apply specs (also static),
		// and writeSystemPackageManifest validates the same set
		// against the regex. The regex `^[a-z0-9][a-z0-9+\-.]*$`
		// rejects every shell-meaningful character, so embedding
		// `pkg` into an ExecBash script is safe here.
		//
		// Force C locale: dpkg-query's "no packages found matching"
		// message is localized on hosts with LANG=ja_JP.UTF-8 / etc.
		// — without pinning the locale, this English-substring match
		// would fail on a non-C runner and we'd Fail() on a clean
		// host that actually had the package absent.
		Expect(systemPackageTestPkgNameRE.MatchString(pkg)).To(BeTrue(),
			"pkgCurrentState called with disallowed pkg name %q", pkg)
		out, err := testExec.ExecBash(`LC_ALL=C LANGUAGE=C dpkg-query -W -f='${db:Status-Status}` + "\n" + `' -- ` + pkg)
		if err == nil {
			return strings.TrimSpace(out)
		}
		// dpkg-query's stable C-locale format for the never-
		// installed / purged case: `dpkg-query: no packages found
		// matching <pkg>` (exit 1). Any other err+output combination
		// indicates a real failure that must surface — fail the
		// spec rather than silently treating it as "not installed".
		if strings.Contains(out, "no packages found matching") {
			return ""
		}
		Fail(fmt.Sprintf("dpkg-query failed unexpectedly for %s (not the documented 'no packages found' case): err=%v, output=%q", pkg, err, out))
		return "" // unreachable: Fail() panics
	}

	// snapshotInstalledPackages returns the set of currently-installed
	// package names on the host. Used by the outer AfterAll (in
	// TOMEI_E2E_CONTAINER mode only) to detect any packages added
	// since BeforeAll — typically transitive Depends pulled in by
	// `apt-get install` for cowsay etc. — so container-mode cleanup
	// can remove them explicitly. Native mode does not run the diff
	// (see the gated block in AfterAll) so a native opt-in run may
	// leak transitive deps; that's an intentional trade-off documented
	// alongside the diff-cleanup site.
	//
	// Returns (set, error) rather than calling Expect so the outer
	// AfterAll's best-effort cleanup contract is preserved: a
	// dpkg-query failure during cleanup is logged and the diff step is
	// skipped, instead of failing the spec (which would mask the
	// underlying assertion that triggered the cleanup).
	//
	// Locale is forced to C so the dpkg-query Status-Status field
	// always emits the un-translated tokens (`installed`,
	// `not-installed`, etc.) the Go strings.Fields filter below
	// matches against.
	snapshotInstalledPackages := func() (map[string]bool, error) {
		out, err := testExec.ExecBash(`LC_ALL=C LANGUAGE=C dpkg-query -W -f='${db:Status-Status} ${binary:Package}` + "\n" + `' 2>/dev/null`)
		if err != nil {
			return nil, fmt.Errorf("dpkg-query enumeration failed: %s: %w", out, err)
		}
		set := map[string]bool{}
		for line := range strings.SplitSeq(out, "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[0] == "installed" {
				set[fields[1]] = true
			}
		}
		return set, nil
	}
	assertInstalled := func(pkg string) {
		// pkgCurrentState now collapses dpkg-query's documented exit-1
		// not-found case to "" (rather than surfacing the stderr
		// diagnostic string CombinedOutput would otherwise yield), so
		// the state-equality check is the single source of truth: any
		// failure to find the package — not-found, dpkg DB error,
		// transient — fails this Expect rather than slipping past via
		// a non-error path.
		state := pkgCurrentState(pkg)
		Expect(state).To(Equal("installed"),
			"package %s should be installed; dpkg-query current-state: %q", pkg, state)
	}
	// removedStates is the explicit allowlist of dpkg current-states that
	// count as "removed" for the purposes of this suite:
	//
	//   - `""`             — dpkg-query exit-1 with empty stdout, meaning
	//                        the package is not in the dpkg DB at all
	//                        (never installed, or purged).
	//   - `not-installed`  — known to dpkg but not currently installed.
	//   - `config-files`   — apt-get remove (without --purge) leaves
	//                        config files behind; binaries are gone.
	//
	// Other current-states (`installed`, `half-installed`, `unpacked`,
	// `half-configured`, `triggers-awaited`, `triggers-pending`) all
	// indicate the package is in a transitional or broken-but-present
	// state on the host — none of those should count as "removed", and
	// the earlier `NotTo(Equal("installed"))` predicate would have let
	// `half-installed` / `unpacked` slip through and mask a failed
	// removal. The explicit allowlist makes the contract one-way:
	// anything not on the list is a failure.
	removedStates := map[string]bool{
		"":              true,
		"not-installed": true,
		"config-files":  true,
	}
	assertNotInstalled := func(pkg string) {
		state := pkgCurrentState(pkg)
		Expect(removedStates).To(HaveKey(state),
			"package %s must be in a known-removed dpkg state (one of %v), got current-state: %q", pkg, removedStates, state)
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
	// SystemPackageRepository apply now lives in the sibling
	// "Apply --system installs SystemPackageRepository (#218)" Context
	// below (gnupg is in the runner image only as the keyring-validation
	// oracle now that apply dearmors in-process, #283; see
	// e2e/containers/ubuntu/Dockerfile). Keeping the SystemPackageSet
	// apply heredocs PGDG-free lets that Context exercise the package-
	// install path in isolation, without reaching apt.postgresql.org
	// over the network on every run.
	// Scratch paths carry a per-process unguessable suffix
	// (PID + ns timestamp). The earlier fixed paths
	// (`/tmp/tomei-system-package-install/`, etc.) were predictable
	// across runs and across users on the same host, so a same-user
	// process could race between the marker check and the rm -rf in
	// writeSystemPackageManifest. With the suffix, no other process
	// can guess the path before this BeforeAll runs, so the TOCTOU
	// window between marker validation and removal collapses — the
	// path is suite-private from the moment of computation.
	scratchSuffix := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	installCfgPath := "/tmp/tomei-system-package-install-" + scratchSuffix + "/"
	removalCfgPath := "/tmp/tomei-system-package-removal-" + scratchSuffix + "/"

	// Source-of-truth constants for the fixture set. The earlier
	// design had three separate hand-maintained lists (fixturePackages
	// for cleanup, the install manifest's hard-coded packages, the
	// removal manifest's hard-coded packages) — those could drift
	// from each other and silently let the preflight/cleanup operate
	// on a different set than the apply specs install. Routing every
	// list through these constants makes drift impossible.
	//
	//   fixtureSetInstall — cli-tools SystemPackageSet members at
	//     install time. Passed to writeSystemPackageManifest for the
	//     install manifest, used by the apply install spec.
	//   fixtureSetRemoval — cli-tools members after the shrink (the
	//     "remove sl, keep cowsay" Context).
	//   fixtureSugarPkg — the SystemPackage sugar entry (a single
	//     package, currently `tree`). Embedded into the generated
	//     manifest's SystemPackage stanza.
	//
	// fixturePackages is the derived union of all packages the apply
	// specs may install — what BeforeAll snapshots, what the outer
	// AfterAll cleans up, and what the cascade-removal simulator
	// passes to apt-get -s remove.
	fixtureSetInstall := []string{"cowsay", "sl"}
	fixtureSetRemoval := []string{"cowsay"}
	const fixtureSugarPkg = "tree"
	// Build fixturePackages as a deduplicated union. A naive
	// `append(..., fixtureSugarPkg)` would emit duplicates if a future
	// change makes the sugar package name overlap with
	// fixtureSetInstall (e.g. someone renames the sugar to "cowsay").
	// Duplicates in the cleanup/preflight command line would not
	// cause incorrect behaviour with apt-get (it tolerates them), but
	// would violate the documented "union" contract and could
	// surprise log readers.
	fixturePackages := func() []string {
		seen := map[string]bool{}
		var out []string
		for _, p := range fixtureSetInstall {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
		if !seen[fixtureSugarPkg] {
			out = append(out, fixtureSugarPkg)
		}
		return out
	}()

	// preflightComplete is set to true at the very end of Context A's
	// BeforeAll, after the remove-first preflight has succeeded. It is
	// the gate that authorizes the outer AfterAll to apt-get remove the
	// fixture packages from the host.
	//
	// Outer AfterAll consults it: with preflightComplete=false, the
	// fixture-package remove path is skipped (no `apt-get remove`
	// against the fixture set). However, the RESTORE path is gated
	// independently on preflightMutationStarted (set before the
	// remove-first), so a partial-remove + failed-assertion case
	// still re-installs anything preTestInstalled recorded. So the
	// host-package cleanup is not a wholesale no-op when
	// preflightComplete is false — only the bulk-remove leg is.
	//
	// (`apt-get update` failures are logged via GinkgoWriter and do
	// NOT abort BeforeAll — a stale index is usually still functional
	// — so they are deliberately not in the list of preflight aborts.)
	var preflightComplete bool

	// scratchDirs is a per-directory ownership ledger.
	// writeSystemPackageManifest records each scratch path it has
	// successfully written;
	// the outer AfterAll iterates the recorded keys and rm -rf's only
	// those — never the other suite's path nor a pre-existing /tmp
	// directory the suite did not create. Earlier code used a single
	// boolean flag covering both fixed paths, so a Context A failure
	// before Context B's manifest was written could still trigger an
	// rm -rf of /tmp/tomei-system-package-removal even though this
	// suite never touched it.
	scratchDirs := map[string]bool{}

	// preflightMutationStarted is set RIGHT BEFORE the host-mutating
	// `apt-get remove` runs in Context A's BeforeAll, and stays true
	// even if the subsequent assertions fail. This is the gate for
	// the restore path in the outer AfterAll: we MUST restore any
	// pre-test installed fixture package if the preflight remove was
	// attempted, regardless of whether the assertion loop later passed.
	//
	// Earlier code gated restore on preflightComplete (set only after
	// the assertion loop succeeded), which meant a partial remove
	// followed by a failing assertion would leave pre-existing fixture
	// packages uninstalled with no restore — exactly the host-pollution
	// path the snapshot-and-restore design is meant to close.
	var preflightMutationStarted bool

	// aptCmd wraps an apt-get verb in the same shell prefix the
	// production apt installer uses (see internal/installer/apt/apt.go
	// aptGetEnvPrefix + Client.Install/Remove/SimulateRemoveCascade).
	// The wrapper:
	//   - sudo -n: non-interactive (same NOPASSWD requirement the
	//     SystemInstaller spec commands carry)
	//   - env DEBIAN_FRONTEND=noninteractive: no interactive prompts
	//     from configuration packages (matters for installs that
	//     replace conffiles)
	//   - env LC_ALL=C LANGUAGE=C: stable English output; sudoers with
	//     reset_env would otherwise lose these if set as shell env
	//     vars rather than via `env`
	//   - DPkg::Lock::Timeout=60: wait up to 60s for the dpkg lock
	//     instead of failing immediately on transient apt contention
	//   - --: end-of-options so package names that look like flags
	//     cannot be misinterpreted
	//
	// DUPLICATION NOTE: this string is intentionally a copy of the
	// production wrapper rather than a shared call into internal/
	// installer/apt. The e2e binary is built with the `e2e` tag and
	// must not depend on the production installer's runtime types
	// (Client, executor.Runner, etc.) to avoid pulling in cmd/tomei
	// engine wiring. A future change to the production wrapper
	// (different lock timeout, additional env var, etc.) therefore
	// requires updating BOTH internal/installer/apt/apt.go aptGetEnv
	// Prefix AND this helper in lockstep — there is no compile-time
	// check enforcing the link. The drift detector for that contract
	// is: if production ever stops being NOPASSWD or stops using
	// LC_ALL=C, the apt invocations in this suite would diverge and
	// the CI native legs would surface the inconsistency first.
	aptCmd := func(verb string, pkgs []string) string {
		return "sudo -n env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C apt-get " + verb + " -y -o DPkg::Lock::Timeout=60 -- " + strings.Join(pkgs, " ")
	}

	// preTestInstalled records which fixture packages were ALREADY
	// installed on the host at BeforeAll time, before the preflight
	// remove ran. The outer AfterAll uses it to distinguish packages
	// the test owns (install them; remove on cleanup) from packages
	// the host owned pre-test (remove then re-install on cleanup, so
	// the developer's host state survives the suite). Capture-and-
	// restore is intentionally best-effort: a reinstall failure (e.g.
	// mirror outage, package vanished from the apt cache between
	// BeforeAll and AfterAll) is logged via GinkgoWriter rather than
	// failing the suite at cleanup time, which is more transparent
	// than silently letting the suite uninstall a package the user
	// expected to keep.
	preTestInstalled := map[string]bool{}

	// preTestAutoMarked records which preTestInstalled packages were
	// in apt's "auto-installed" set at BeforeAll time (i.e. apt-mark
	// showauto would list them — these are packages apt brought in
	// as transitive Depends rather than ones the user installed
	// directly). The outer AfterAll's restore path runs `apt-get
	// install`, which marks restored packages as MANUALLY installed
	// by default; without this snapshot, a native opt-in host that
	// originally had cowsay auto-marked would come back with cowsay
	// flagged manual, eligible for the user's next `apt autoremove`
	// to keep when it shouldn't be. Restore therefore also runs
	// `apt-mark auto <pkg>` for the packages recorded here.
	preTestAutoMarked := map[string]bool{}

	// preTestPackageSet is a snapshot of EVERY package installed on
	// the host at BeforeAll time (not just the fixture set). In
	// TOMEI_E2E_CONTAINER mode the outer AfterAll diffs this against
	// the post-test snapshot to identify transitive dependencies
	// pulled in by tomei's apt-get install (e.g. libtext-charwidth-
	// perl for cowsay) and removes them explicitly. This replaces the
	// earlier blanket `apt-get autoremove` which Copilot flagged as
	// too broad.
	//
	// In native mode the diff is intentionally NOT run — a user/
	// system could install packages concurrently during the E2E
	// window and the diff cannot distinguish those from suite-
	// introduced deps. Native mode therefore may leak cowsay's
	// transitive Depends; that's an opt-in trade-off (CI native is
	// ephemeral, dev laptop signed up for best-effort host mutation).
	var preTestPackageSet map[string]bool

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
		// Two-phase setup: claim ownership (rm-if-marker + mkdir +
		// marker touch), record in scratchDirs, then write content.
		// Recording the ledger entry BETWEEN the two phases means a
		// failure during content write (module.cue or manifest.cue
		// heredoc) still leaves the dir tracked, so AfterAll cleans
		// it up rather than leaking the partial scratch dir.
		//
		// Phase 1 — claim. Refuse to rm -rf an existing dir that
		// doesn't carry our ownership marker. With the PID/timestamp
		// suffix on installCfgPath/removalCfgPath, a fresh suite run
		// computes an unguessable path so no other process can race
		// for it; the marker check stays as defense-in-depth against
		// (a) a future refactor that drops the suffix and reverts to
		// fixed names, and (b) the edge case of two suite runs from
		// the same PID with the same nanosecond timestamp (effectively
		// impossible, but the marker keeps the safety net cheap).
		// `set -euo pipefail` so any setup failure (rm -rf refusal,
		// mkdir failure, marker touch failure) aborts the script.
		claim := fmt.Sprintf(`set -euo pipefail
if [ -e %[1]s ] && [ ! -f %[1]s/.tomei-e2e-system-package-test ]; then
	echo "refusing to remove %[1]s: directory exists but lacks the .tomei-e2e-system-package-test ownership marker — another process may own this path" >&2
	exit 1
fi
rm -rf %[1]s
mkdir -p %[1]s/cue.mod
touch %[1]s/.tomei-e2e-system-package-test
`, dir)
		_, err := testExec.ExecBash(claim)
		Expect(err).NotTo(HaveOccurred(), "claiming scratch dir %s failed", dir)

		// Record ownership in the ledger BEFORE writing content. If
		// the subsequent heredoc writes fail, the dir is still
		// tracked and AfterAll will clean it up.
		scratchDirs[strings.TrimRight(dir, "/")] = true

		// Phase 2 — content. Heredoc-write module.cue + manifest.cue.
		// Failure here is surfaced via Expect; the ledger entry above
		// guarantees AfterAll still cleans up the partial dir.
		content := fmt.Sprintf(`set -euo pipefail
cat > %[1]s/cue.mod/module.cue <<'EOF'
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

"%[3]s": {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "SystemPackage"
	metadata: name: "%[3]s"
	spec: {
		installerRef: "apt"
		package:      "%[3]s"
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
EOF`, dir, strings.Join(quoted, ", "), fixtureSugarPkg)
		_, err = testExec.ExecBash(content)
		Expect(err).NotTo(HaveOccurred(), "writing manifest content for %s failed", dir)
	}

	// stateJSONPath is the literal path string with a leading `~`.
	// Tilde expansion is a shell feature performed at parse time on
	// the command line, not by individual commands — so the path must
	// be interpolated into a shell command via ExecBash (where the
	// user's $HOME inside the container may differ from the host test
	// process's $HOME, so locally os.ExpandEnv would compute the wrong
	// path). It is safe with any command that consumes argv from a
	// shell parse (cat, ls, sha256sum, test, etc.); it is NOT safe
	// passed directly as an argv element to argv-form Exec.
	const stateJSONPath = "~/.local/share/tomei/system/state.json"

	// pathExistsAny reports whether ANY filesystem entry sits at path
	// (regular file, directory, live symlink, or dangling symlink). It
	// does NOT read the file, so an unreadable root-owned file or a
	// dangling symlink whose target is missing still classifies as
	// "exists". Use this in safety guards that only need the existence
	// bit and must not abort on hash/permission errors — e.g. the
	// pre-test "refuse to clobber" check in the #218 BeforeAll.
	pathExistsAny := func(path string) bool {
		out, err := testExec.ExecBash(fmt.Sprintf(
			"if [ -e %[1]s ] || [ -L %[1]s ]; then echo yes; else echo no; fi", path))
		Expect(err).NotTo(HaveOccurred(),
			"probing existence of %s failed: %s", path, out)
		return strings.TrimSpace(out) == "yes"
	}

	// fileSha256IfExists.
	//
	// Returns (hash, exists) for a file the SUITE itself wrote and
	// expects to be present. Current use is the post-install snapshot
	// in the #218 install spec, which captures the just-installed
	// keyring/sources.list bytes so the no-flag retention spec can
	// later assert byte-identity. Pre-existing-path safety guards in
	// BeforeAll use pathExistsAny instead — that path must NEVER hash
	// a host-owned file. Hashing/probing errors here are surfaced via
	// Expect (not collapsed to "absent") so a regression in the suite-
	// owned file is loud rather than silent.
	fileSha256IfExists := func(path string) (string, bool) {
		// Stage 1: existence probe. `-e` follows symlinks (and so
		// returns false for a dangling symlink); pair with `-L` so a
		// dangling symlink at this path is still classified as
		// "exists" — defensive against an out-of-band tampering of
		// the suite-owned file between install and snapshot.
		existsOut, existsErr := testExec.ExecBash(fmt.Sprintf(
			"if [ -e %[1]s ] || [ -L %[1]s ]; then echo yes; else echo no; fi", path))
		Expect(existsErr).NotTo(HaveOccurred(),
			"probing existence of %s failed: %s", path, existsOut)
		if strings.TrimSpace(existsOut) == "no" {
			return "", false
		}
		// Stage 2: hash. Only hash regular files (or symlinks resolving
		// to one); dangling symlinks / directories return ("", true)
		// so callers can still see the path exists without us
		// fabricating a hash. Hashing failures on a regular file are
		// fatal — see helper docstring.
		regOut, _ := testExec.ExecBash(fmt.Sprintf(
			"if [ -f %[1]s ]; then echo yes; else echo no; fi", path))
		if strings.TrimSpace(regOut) != "yes" {
			return "", true
		}
		out, err := testExec.ExecBash(fmt.Sprintf(
			"set -euo pipefail; sha256sum -- %[1]s | awk '{print $1}'", path))
		Expect(err).NotTo(HaveOccurred(),
			"%s exists but sha256sum failed: %s", path, out)
		return strings.TrimSpace(out), true
	}

	// resetSystemState writes a minimal valid empty SystemState to the
	// system state.json. Used by Ordered Contexts that need a clean slate
	// without doing a full `tomei init --force` (which would also touch the
	// user state.json owned by other suites).
	resetSystemState := func() {
		_, err := testExec.ExecBash(
			`mkdir -p ~/.local/share/tomei/system && echo '{"version":"1","systemInstallers":{},"systemPackageRepositories":{},"systemPackages":{}}' > ` + stateJSONPath)
		Expect(err).NotTo(HaveOccurred(), "resetSystemState: failed to overwrite %s", stateJSONPath)
	}

	// writeSystemPackageRepositoryManifest writes a /tmp scratch manifest
	// containing the canonical apt SystemInstaller and, when withRepo=true,
	// the pgdgRepo SystemPackageRepository (matching the keyHash + suite
	// pinned in the rotation-contract consts). withRepo=false emits the
	// installer only — used by the removal and retain-without-flag specs.
	//
	// Mirrors writeSystemPackageManifest's two-phase TOCTOU-safe setup:
	// claim ownership via marker file, record in scratchDirs, then write
	// content. Validates dir against systemPackageTestDirRE.
	writeSystemPackageRepositoryManifest := func(dir string, withRepo bool) {
		Expect(systemPackageTestDirRE.MatchString(dir)).To(BeTrue(),
			"dir %q does not match the absolute-path allowlist; only static /tmp paths are accepted by this helper", dir)
		claim := fmt.Sprintf(`set -euo pipefail
if [ -e %[1]s ] && [ ! -f %[1]s/.tomei-e2e-system-package-test ]; then
	echo "refusing to remove %[1]s: directory exists but lacks the .tomei-e2e-system-package-test ownership marker — another process may own this path" >&2
	exit 1
fi
rm -rf %[1]s
mkdir -p %[1]s/cue.mod
touch %[1]s/.tomei-e2e-system-package-test
`, dir)
		_, err := testExec.ExecBash(claim)
		Expect(err).NotTo(HaveOccurred(), "claiming scratch dir %s failed", dir)
		scratchDirs[strings.TrimRight(dir, "/")] = true

		repoBlock := ""
		if withRepo {
			repoBlock = fmt.Sprintf(`
pgdgRepo: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "SystemPackageRepository"
	metadata: name: "pgdg"
	spec: {
		installerRef: "apt"
		apt: {
			url:     "%[3]s"
			keyUrl:  "%[4]s"
			keyHash: "%[1]s"
			suite:   "%[2]s"
			components: ["%[5]s"]
		}
	}
}
`, pgdgKeyHashSHA256, pgdgSuite, pgdgURL, pgdgKeyURL, pgdgComponent)
		}

		content := fmt.Sprintf(`set -euo pipefail
cat > %[1]s/cue.mod/module.cue <<'EOF'
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
%[2]s
EOF`, dir, repoBlock)
		_, err = testExec.ExecBash(content)
		Expect(err).NotTo(HaveOccurred(), "writing repository manifest content for %s failed", dir)
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
			// Four independent cleanup paths follow, each with its
			// own ownership-tracking flag:
			//
			//   1. preflightComplete → fixture-package remove. Acts
			//      only when the apply specs may have run install.
			//   2. preflightMutationStarted + TOMEI_E2E_CONTAINER →
			//      diff-based dep cleanup (snapshotInstalledPackages
			//      before/after). Acts only in container mode where
			//      newly-installed packages are unambiguously tomei's.
			//      Native mode intentionally skips this step (concurrent
			//      installs during the E2E window cannot be distinguished
			//      from suite-introduced deps).
			//   3. preflightMutationStarted → restore preTestInstalled.
			//      Acts whenever the mutating preflight was attempted,
			//      so a partial remove still re-installs pre-existing
			//      fixture packages even when post-remove assertions
			//      failed and preflightComplete stayed false.
			//   4. scratchDirs (per-dir ledger) → rm -rf the scratch
			//      directories. Acts only on paths the suite wrote, so
			//      a pre-existing /tmp dir owned by another process is
			//      never touched.
			//
			// Splitting the gates was Copilot's repeated ask: the
			// earlier single-flag design either leaked dirs (when the
			// preflight failed after the manifest was written) or
			// failed to restore pre-test host packages (when the
			// preflight removed some but not all, then assertion-
			// failed).
			if preflightComplete {
				// Full cleanup path: the preflight succeeded AND specs
				// may have run apply, so tomei may have installed the
				// fixture packages. Remove them by explicit name (no
				// blast-radius broadening) before the restore step
				// re-installs the host's pre-test set. Capture stdout
				// + err so a remove failure (apt lock, sudo timestamp
				// expired between specs and AfterAll, etc.) is logged
				// to GinkgoWriter rather than silently leaving fixture
				// packages installed.
				//
				// Re-run the cascade-removal simulation BEFORE the
				// actual remove. On a long-running native opt-in host,
				// a package installed CONCURRENTLY with the e2e test
				// could have started depending on cowsay/sl/tree
				// between the BeforeAll simulation and this AfterAll
				// (the BeforeAll check was a snapshot, not a hold).
				// If the cleanup remove would now cascade onto a non-
				// fixture package, the restore path doesn't track it
				// and would leave the host worse off than pre-test —
				// log the cascade and skip the cleanup remove instead.
				simOut, simErr := testExec.ExecBash(aptCmd("-s remove", fixturePackages) + " 2>&1")
				if simErr != nil {
					fmt.Fprintf(GinkgoWriter, "WARNING: cascade simulation in AfterAll failed (err=%v), skipping fixture remove to avoid host damage:\n%s\n", simErr, simOut)
				} else {
					fixtureSet := map[string]bool{}
					for _, p := range fixturePackages {
						fixtureSet[p] = true
					}
					var cascade []string
					for line := range strings.SplitSeq(simOut, "\n") {
						if !strings.HasPrefix(line, "Remv ") {
							continue
						}
						fields := strings.Fields(line)
						if len(fields) >= 2 && !fixtureSet[fields[1]] {
							cascade = append(cascade, fields[1])
						}
					}
					sort.Strings(cascade)
					if len(cascade) > 0 {
						fmt.Fprintf(GinkgoWriter, "WARNING: AfterAll fixture-package remove would cascade-remove non-fixture package(s) %v — skipping cleanup remove. The restore path will still re-install preTestInstalled. Full simulation output:\n%s\n", cascade, simOut)
					} else {
						out, err := testExec.ExecBash(aptCmd("remove", fixturePackages) + " 2>&1")
						if err != nil {
							fmt.Fprintf(GinkgoWriter, "WARNING: fixture-package cleanup failed (err=%v):\n%s\n", err, out)
						}
					}
				}
			}
			if preflightMutationStarted && preTestPackageSet != nil && os.Getenv("TOMEI_E2E_CONTAINER") != "" {
				// Diff-based dep cleanup, scoped to TOMEI_E2E_CONTAINER
				// ONLY. In container mode we own the entire FS, so any
				// package now present that wasn't pre-test is by
				// definition something tomei installed (cowsay pulled
				// libtext-charwidth-perl, etc.) — safe to remove.
				//
				// In native mode (CI native legs OR developer laptop)
				// the user/system could install packages concurrently
				// during the E2E window, and the diff would falsely
				// attribute those to the suite. The trade-off: native
				// mode may leak cowsay's transitive Depends across
				// suite runs. That's acceptable because the CI native
				// runner is ephemeral and a developer on
				// TOMEI_E2E_NATIVE=true has opted in to best-effort
				// host mutation.
				//
				// snapshotInstalledPackages returns an error here
				// instead of calling Expect, so a dpkg-query failure
				// inside cleanup is logged and the diff step is
				// skipped — never failing the spec from within
				// AfterAll (which would mask the original assertion
				// failure that triggered the cleanup).
				currentSet, snapErr := snapshotInstalledPackages()
				if snapErr != nil {
					fmt.Fprintf(GinkgoWriter, "WARNING: diff-based dep cleanup skipped: %v\n", snapErr)
				} else {
					var leaked []string
					for pkg := range currentSet {
						if !preTestPackageSet[pkg] {
							leaked = append(leaked, pkg)
						}
					}
					sort.Strings(leaked)
					if len(leaked) > 0 {
						fmt.Fprintf(GinkgoWriter, "removing %d package(s) introduced by the suite: %v\n", len(leaked), leaked)
						out, err := testExec.ExecBash(aptCmd("remove", leaked) + " 2>&1")
						if err != nil {
							fmt.Fprintf(GinkgoWriter, "WARNING: diff-based dep cleanup remove failed (err=%v):\n%s\n", err, out)
						}
					}
				}
			}
			if preflightMutationStarted {
				// Restore path runs whenever the preflight remove was
				// ATTEMPTED — not only when the full preflight succeeded.
				// A partial remove + failed assertion would otherwise
				// leave pre-existing fixture packages uninstalled with
				// no restore. Iterate in fixturePackages order (rather
				// than map iteration order) so the apt-get install
				// command line is deterministic across runs.
				var toRestore []string
				for _, pkg := range fixturePackages {
					if preTestInstalled[pkg] {
						toRestore = append(toRestore, pkg)
					}
				}
				if len(toRestore) > 0 {
					// Restore uses the production wrapper too (same env,
					// same lock timeout). --no-install-recommends keeps
					// reinstall scope narrow: we restore exactly what
					// was on the host pre-test, not Recommends that
					// might also have been there but cannot be
					// reliably reconstructed. The flag goes inside the
					// verb so it lands BEFORE the `--` end-of-options
					// separator added by aptCmd.
					//
					// After reinstall, re-apply apt's "auto" mark for
					// packages that were auto-installed pre-test
					// (apt-get install marks restored packages as
					// MANUAL by default, which would otherwise change
					// the host's apt-mark state on net even though the
					// installed set is restored). Determined from
					// preTestAutoMarked captured at BeforeAll.
					out, err := testExec.ExecBash(aptCmd("install --no-install-recommends", toRestore) + " 2>&1")
					if err != nil {
						// Best-effort: log and continue. A reinstall
						// failure (e.g. mirror outage, package vanished
						// from the index between BeforeAll and AfterAll)
						// is more transparent than failing the whole
						// suite at cleanup time; the developer can
						// re-run apt-get install manually if needed.
						fmt.Fprintf(GinkgoWriter, "WARNING: failed to restore pre-test packages %v (err=%v):\n%s\n", toRestore, err, out)
					}
					// Re-apply auto marks for packages that were
					// auto-installed pre-test. apt-mark auto is
					// idempotent and unprivileged-friendly under sudo
					// (no -n needed because apt-mark itself doesn't
					// touch the dpkg lock the way apt-get does, but we
					// keep -n for parity). Iterate in fixturePackages
					// order for deterministic output.
					var toAutoMark []string
					for _, pkg := range fixturePackages {
						if preTestAutoMarked[pkg] {
							toAutoMark = append(toAutoMark, pkg)
						}
					}
					if len(toAutoMark) > 0 {
						mOut, mErr := testExec.ExecBash("sudo -n apt-mark auto -- " + strings.Join(toAutoMark, " ") + " 2>&1")
						if mErr != nil {
							fmt.Fprintf(GinkgoWriter, "WARNING: failed to re-apply auto marks for %v (err=%v):\n%s\n", toAutoMark, mErr, mOut)
						}
					}
				}
			}
			// Scratch-dir cleanup iterates the per-dir ownership ledger
			// (scratchDirs) rather than blindly removing both fixed
			// paths. So if Context A wrote its dir and aborted before
			// Context B wrote its own, only Context A's dir is removed
			// — a pre-existing /tmp/tomei-system-package-removal owned
			// by something else is not touched.
			for dir := range scratchDirs {
				// Defense-in-depth: re-validate the recorded dir
				// against the helper's regex before rm -rf.
				if !systemPackageTestDirRE.MatchString(dir) {
					continue
				}
				// Marker re-check: only rm if our ownership marker
				// is still present at the recorded path. If a
				// concurrent process replaced the directory between
				// BeforeAll and AfterAll, the marker is gone and we
				// skip cleanup rather than clobbering a directory the
				// suite no longer owns. The marker was written under
				// `set -euo pipefail` in writeSystemPackageManifest
				// immediately after mkdir, so any successful entry in
				// scratchDirs implies the marker was there at write
				// time — re-checking here closes the TOCTOU window.
				_, markerErr := testExec.ExecBash(`test -f ` + dir + `/.tomei-e2e-system-package-test`)
				if markerErr != nil {
					fmt.Fprintf(GinkgoWriter, "skipping rm -rf %s: ownership marker missing (dir may have been replaced by another process)\n", dir)
					continue
				}
				_, _ = testExec.ExecBash("rm -rf " + dir)
			}
		})

		Context("Apply --system (real install)", func() {
			BeforeAll(func() {
				skipIfNotLinux()

				// Probe non-interactive sudo BEFORE touching host state.
				// Outer AfterAll's apt-get remove uses `sudo -n`; on a
				// native opt-in run with interactive-only sudo, that
				// cleanup would no-op and leave cowsay/sl/tree
				// installed. Fail fast here instead.
				//
				// `sudo -k && sudo -n true` is the correct probe: a
				// bare `sudo -n true` succeeds when the user has a
				// cached sudo timestamp from earlier in the session,
				// which would falsely pass on a host that actually
				// requires a password. -k invalidates the cache first,
				// so the subsequent -n true exercises the NOPASSWD
				// policy that the apply specs (which themselves call
				// `sudo -k` between runs) actually depend on.
				_, sudoErr := testExec.ExecBash("sudo -k && sudo -n true 2>&1")
				Expect(sudoErr).NotTo(HaveOccurred(),
					"non-interactive sudo (NOPASSWD) is required: the AfterAll cleanup uses `sudo -n` and would silently leave fixture packages installed without it. The probe ran `sudo -k && sudo -n true` to invalidate any cached timestamp first.")

				// Ensure tomei is initialized — `tomei apply` aborts early
				// with "tomei is not initialized" otherwise. --force makes
				// the call idempotent across prior Contexts. Assert
				// success here: silently ignoring the error would let the
				// host-mutating preflight below run even when the
				// downstream apply spec is guaranteed to fail with
				// "tomei is not initialized", uninstalling fixture
				// packages without ever exercising the install path.
				initOut, initErr := testExec.Exec("tomei", "init", "--yes", "--force")
				Expect(initErr).NotTo(HaveOccurred(), "tomei init failed before preflight: %s", initOut)

				// Reset SYSTEM state only — user-store belongs to other suites.
				// Top-level JSON keys match internal/state/state.go SystemState.
				// Failure here means the home directory is misconfigured and
				// every downstream spec would surface a misleading error, so
				// fail loudly instead of swallowing the error.
				_, err := testExec.ExecBash(`mkdir -p ~/.local/share/tomei/system && echo '{"version":"1","systemInstallers":{},"systemPackageRepositories":{},"systemPackages":{}}' > ~/.local/share/tomei/system/state.json`)
				Expect(err).NotTo(HaveOccurred(), "failed to reset system state.json")

				// Ordering matters: do every non-mutating setup step FIRST,
				// THEN the host-mutating preflight remove, THEN set
				// preflightComplete. If a non-mutating step fails it does so
				// before any host package has been touched, so
				// preflightComplete stays false, the outer AfterAll skips
				// cleanup, and a pre-existing host install of cowsay/sl/tree
				// survives. The earlier ordering performed the remove BEFORE
				// writing the manifest and running apt-get update — a write
				// or update failure between those two steps would have left
				// a developer's previously-installed package uninstalled
				// with no record of what to restore.

				// (1) Non-mutating: write the generated /tmp manifest.
				writeSystemPackageManifest(strings.TrimRight(installCfgPath, "/"), fixtureSetInstall)

				// (2) Non-mutating from the host-package perspective: refresh
				// the apt index. PackageSetInstaller.Install does not refresh
				// it itself (PackageRepositoryInstaller does, but only that
				// one). CI images carry stale indexes; refresh once. Tolerate
				// non-zero exit — apt exits non-zero on any mirror failure,
				// but a partial refresh is usually enough for the apply step
				// to produce a clearer downstream error than `update` would.
				// The err is captured (not silently dropped) so a complete
				// mirror outage surfaces in GinkgoWriter alongside the output.
				out, err := testExec.ExecBash("sudo -n env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C apt-get update -qq -o DPkg::Lock::Timeout=60 2>&1")
				fmt.Fprintf(GinkgoWriter, "apt-get update (err=%v):\n%s\n", err, out)

				// (3a) Snapshot which fixture packages are already installed
				// on the host BEFORE the preflight remove runs. The outer
				// AfterAll uses this to reinstall any package that was
				// here pre-test, so a native opt-in run leaves the
				// developer/runner's host state untouched on net
				// (uninstall + reinstall = no-op modulo version drift).
				// Captured before the remove so we can distinguish
				// "tomei installed this" from "the host had this before".
				for _, pkg := range fixturePackages {
					if pkgCurrentState(pkg) == "installed" {
						preTestInstalled[pkg] = true
					}
				}

				// (3a*) Also snapshot apt-mark auto state for the
				// pre-installed packages, so the AfterAll restore can
				// re-apply auto-marks. apt-mark showauto is unprivileged
				// and never prompts; failures here are non-fatal (no
				// auto-marks captured → restore re-installs as manual,
				// closer to but not exactly the pre-test state). C
				// locale not strictly needed (output is package-name
				// list with no localized phrases) but kept for parity.
				autoOut, autoErr := testExec.ExecBash("LC_ALL=C LANGUAGE=C apt-mark showauto 2>&1")
				// Fail fast (rather than log-and-continue) if the
				// auto-mark snapshot can't be taken: continuing with
				// an empty preTestAutoMarked would let the AfterAll
				// restore re-install pre-existing fixture packages as
				// MANUAL, silently changing apt's auto/manual metadata
				// on a native opt-in host that had them auto-marked.
				// Aborting BeforeAll here happens BEFORE the preflight
				// remove runs, so no host package state has been
				// touched yet — preflightMutationStarted stays false
				// and AfterAll is a no-op.
				Expect(autoErr).NotTo(HaveOccurred(),
					"apt-mark showauto failed before preflight remove: %s\nrefusing to mutate host packages without a complete auto-mark snapshot for restore", autoOut)
				autoSet := map[string]bool{}
				for line := range strings.SplitSeq(autoOut, "\n") {
					if name := strings.TrimSpace(line); name != "" {
						autoSet[name] = true
					}
				}
				for _, pkg := range fixturePackages {
					if preTestInstalled[pkg] && autoSet[pkg] {
						preTestAutoMarked[pkg] = true
					}
				}

				// (3a') Take a full host-wide installed-packages
				// snapshot too, for the dep-leak diff in AfterAll.
				// Captured here (after fixture preflight snapshot but
				// before remove) so the post-test diff isolates packages
				// added by tomei's install step. BeforeAll Expects on
				// the error: a failed baseline snapshot would silently
				// disable the diff cleanup, defeating the purpose, so
				// fail fast here rather than at AfterAll time.
				var snapErr error
				preTestPackageSet, snapErr = snapshotInstalledPackages()
				Expect(snapErr).NotTo(HaveOccurred(),
					"baseline snapshotInstalledPackages failed — cannot establish a pre-test package set for the AfterAll diff cleanup")

				// (3b) Mutating: preflight remove-first. Ensures the fixture
				// packages are NOT installed before tomei runs, so the
				// subsequent `apply --system` actually exercises the install
				// path rather than passing because the package happened to
				// already be on the runner.
				//
				// History: an earlier version used a hard fail-on-preinstalled
				// invariant which broke the linux/arm64 native CI leg because
				// GH-hosted ubuntu-24.04 runners preinstall `bc`. Switching
				// to cowsay/sl (universe, leaf, NOT in any known runner-image
				// preinstall list) plus this remove-first step makes the spec
				// robust against future preinstall drift on either side. We
				// use NOT-purge so /etc/cowsay (none exist, but futureproof)
				// is preserved; the outer AfterAll handles final removal.
				//
				// Cascade-removal probe. Simulate the remove (`apt-get
				// -s`) BEFORE mutating anything: if apt reports any
				// non-fixture package would be removed as a reverse-
				// dependency of cowsay/sl/tree, abort the spec rather
				// than uninstalling host state the restore path
				// doesn't track. cowsay/sl/tree are leaf packages today,
				// so the simulation reports only the fixture set; this
				// is defense-in-depth for any future fixture change.
				// Matches the production PackageSetInstaller's pre-
				// remove simulation.
				//
				// Capture the simulation's exit code: a non-zero exit
				// means apt failed to even compute the plan (lock held,
				// broken dependency DB, etc.) and the output cannot be
				// trusted as "no cascade detected". Abort the spec
				// before the real mutating remove runs. (apt-get -s
				// remove returns 0 even when packages are not
				// installed — that case is benign and produces no Remv
				// lines, so we don't need to special-case it.)
				simOut, simErr := testExec.ExecBash(aptCmd("-s remove", fixturePackages) + " 2>&1")
				Expect(simErr).NotTo(HaveOccurred(),
					"apt-get -s remove simulation failed — cannot verify the real remove will not cascade. Aborting before host mutation. Full output:\n%s", simOut)
				fixtureSet := map[string]bool{}
				for _, p := range fixturePackages {
					fixtureSet[p] = true
				}
				var cascade []string
				for line := range strings.SplitSeq(simOut, "\n") {
					// `apt-get -s remove` prints `Remv <pkg> [<version>]`
					// for each package that would actually be removed.
					if !strings.HasPrefix(line, "Remv ") {
						continue
					}
					fields := strings.Fields(line)
					if len(fields) < 2 {
						continue
					}
					if !fixtureSet[fields[1]] {
						cascade = append(cascade, fields[1])
					}
				}
				sort.Strings(cascade)
				Expect(cascade).To(BeEmpty(),
					"preflight remove would cascade-remove non-fixture package(s) %v — aborting before any host mutation so the restore path (which only knows about fixturePackages) cannot lose unrelated host state. Full apt-get -s remove output:\n%s",
					cascade, simOut)

				// Mark mutation-started BEFORE the remove runs. This
				// authorizes the AfterAll restore path independently of
				// whether the post-remove assertions pass — if apt
				// removes one pre-existing fixture package and then
				// fails on another, preflightComplete stays false but
				// preflightMutationStarted is true, so the restore loop
				// still re-installs whatever preTestInstalled recorded.
				// Without this split, a partially-failed preflight could
				// leave the host with a permanently uninstalled package
				// that was there before the suite ran.
				preflightMutationStarted = true
				// `2>&1 || true` swallows the failure for packages that are
				// not installed (apt-get remove exits non-zero); the
				// pkgCurrentState assertions below are the real assertion.
				_, _ = testExec.ExecBash(aptCmd("remove", fixturePackages) + " 2>&1 || true")
				for _, pkg := range fixturePackages {
					// Query the current-state sub-field and check it against
					// the same removedStates allowlist assertNotInstalled
					// uses — a held installed package (Status `hold ok
					// installed`) or a broken half-installed state both fail
					// here, so neither can slip past the preflight and make
					// the install spec a no-op.
					state := pkgCurrentState(pkg)
					Expect(removedStates).To(HaveKey(state),
						"preflight failed: %s is not in a known-removed dpkg state after apt-get remove (current-state: %q, expected one of %v) — the apply spec below would not be exercising a real install", pkg, state, removedStates)
				}

				// (4) Mark preflight complete. From this point the outer
				// AfterAll is allowed to run apt-get remove against the
				// fixture packages — the preflight already uninstalled any
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
				// Parse the JSON rather than substring-matching "cowsay" / "sl":
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
				// Anchor to end-of-line with `(?m)$`. The earlier
				// `\b` form matched at the comma boundary, so a
				// trailing `, N disabled` segment (rendered only when
				// ActionSkip > 0) would have satisfied the regex —
				// meaning a regression where SystemPackageSet starts
				// being skip-downgraded again with --system would pass
				// silently. Adding a separate ContainSubstring
				// negation on "disabled" closes the loop in case the
				// printer rephrases the suffix.
				Expect(out).To(MatchRegexp(`(?m)^Summary:\s+0 to install,\s+0 to upgrade,\s+0 to reinstall,\s+0 to remove\s*$`),
					"plan summary must end at '0 to remove' with no trailing ', N disabled' counter; got:\n%s", out)
				Expect(out).NotTo(ContainSubstring("disabled"),
					"plan output must not contain 'disabled' (SystemPackageSet must not be skip-downgraded with --system); got:\n%s", out)

				hashBefore := stateHash()
				idempOut, err := ExecApply(testExec, "--system", installCfgPath)
				Expect(err).NotTo(HaveOccurred())
				// runApplyWithProgressManager (cmd/tomei/apply.go) logs
				// system-engine errors as `Warning: system resource
				// apply failed: ...` and returns the user-engine
				// result. So err == nil does NOT prove the system
				// engine succeeded; a silent system-apply regression
				// would otherwise sail through this idempotency check
				// because state.json would also remain unchanged on
				// failure. Anchoring on the absence of the warning
				// substring closes that gap.
				Expect(idempOut).NotTo(ContainSubstring("system resource apply failed"),
					"idempotent re-apply must not surface a system-engine warning; got:\n%s", idempOut)
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
				// the prior validate / plan Contexts that read
				// ~/system-package-test/manifest.cue keep seeing a
				// byte-stable manifest. (Context A applies its own
				// generated /tmp/tomei-system-package-install-<pid>-<ns>/
				// path, not the canonical fixture, so it doesn't appear
				// in this list.)
				writeSystemPackageManifest(strings.TrimRight(removalCfgPath, "/"), fixtureSetRemoval)
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
				// Parity with the install-path coverage: a sibling
				// SystemPackageSet shrink must not corrupt the
				// surviving SystemPackage sugar entry's recorded
				// version. An empty version here would indicate the
				// shrink path rewrote tree.installedVersions["tree"]
				// to "" instead of leaving it byte-stable.
				Expect(installed["tree"]["tree"]).NotTo(BeEmpty(),
					"tree.installedVersions.tree must remain a non-empty version string across a sibling set's shrink")
			})

			It("re-apply removal manifest WITH --system is idempotent", func() {
				// Mirrors Context A's idempotency: once the removal has
				// converged, a second --system apply against the same reduced
				// manifest must be a plan-zero / state-byte-stable no-op.
				out, err := testExec.Exec("tomei", "plan", "--system", removalCfgPath)
				Expect(err).NotTo(HaveOccurred())
				// Anchor to end-of-line with `(?m)$`. The earlier
				// `\b` form matched at the comma boundary, so a
				// trailing `, N disabled` segment (rendered only when
				// ActionSkip > 0) would have satisfied the regex —
				// meaning a regression where SystemPackageSet starts
				// being skip-downgraded again with --system would pass
				// silently. Adding a separate ContainSubstring
				// negation on "disabled" closes the loop in case the
				// printer rephrases the suffix.
				Expect(out).To(MatchRegexp(`(?m)^Summary:\s+0 to install,\s+0 to upgrade,\s+0 to reinstall,\s+0 to remove\s*$`),
					"plan summary must end at '0 to remove' with no trailing ', N disabled' counter; got:\n%s", out)
				Expect(out).NotTo(ContainSubstring("disabled"),
					"plan output must not contain 'disabled' (SystemPackageSet must not be skip-downgraded with --system); got:\n%s", out)

				hashBefore := stateHash()
				idempOut, err := ExecApply(testExec, "--system", removalCfgPath)
				Expect(err).NotTo(HaveOccurred())
				// Same system-engine warning check as the install
				// idempotency spec above — runApplyWithProgressManager
				// returns the user-engine result, so a silent system
				// apply failure would also leave state.json unchanged
				// and falsely pass the byte-stability assertion.
				Expect(idempOut).NotTo(ContainSubstring("system resource apply failed"),
					"idempotent re-apply of the removal manifest must not surface a system-engine warning; got:\n%s", idempOut)
				Expect(stateHash()).To(Equal(hashBefore),
					"idempotent re-apply of the removal manifest must not rewrite state.json")
			})
		})
	})

	// SystemPackageRepository apply coverage (#218). Sibling to #200 above;
	// mutates host state (/usr/share/keyrings/pgdg.gpg + /etc/apt/sources.list.d/pgdg.list)
	// and reaches apt.postgresql.org on apply, so gated to linux + opt-in
	// e2e (TOMEI_E2E_CONTAINER / TOMEI_E2E_NATIVE) via skipIfNotLinux.
	//
	// Spec ordering under Ordered Context is cumulative:
	//   1. install         — writes pgdg files + state.json entry
	//   2. idempotency     — depends on spec 1's mutation
	//   3. retain-without-flag (#169 scenario 7) — depends on spec 1's mutation
	//   4. removal         — tears down what spec 1 built
	//
	// BeforeAll Skips the Context if either pgdgKeyringPath or
	// pgdgSourcePath already exists on the host (including as a
	// dangling symlink) — see the pathExistsAny guard. With that gate
	// in place, any file present at AfterAll time was written by this
	// Context and is unconditionally safe to remove; no restore step
	// is needed. TOMEI_E2E_NATIVE_SKIP_CLEANUP=true bypasses the
	// AfterAll cleanup to preserve host state for inspection after a
	// failed native opt-in run (same semantics as the sibling #200
	// Context).
	Context("Apply --system installs SystemPackageRepository (#218)", Ordered, func() {
		const (
			pgdgKeyringPath = "/usr/share/keyrings/pgdg.gpg"
			pgdgSourcePath  = "/etc/apt/sources.list.d/pgdg.list"
		)

		var (
			repoCfgPath           string // manifest WITH pgdgRepo (specs 1 & 2)
			installerOnlyCfgPath  string // manifest WITHOUT pgdgRepo (specs 3 & 4)
			pgdgPreflightComplete bool   // gate for AfterAll: BeforeAll ran far enough to need cleanup
			pgdgMutationStarted   bool   // gate for AfterAll: spec 1 actually invoked apply --system
			// Post-install file hashes captured at the end of spec 1.
			// Spec 3 ("apply WITHOUT --system retains state") compares
			// against these to verify a no-flag apply truly leaves the
			// keyring + sources.list bytes untouched (existence-only
			// checks would miss a regression that rewrites the files
			// while preserving their paths).
			postInstallFileHashes map[string]string
		)
		// repoScratchDirPrefix matches /tmp scratch dirs registered by
		// writeSystemPackageRepositoryManifest into the suite-level
		// scratchDirs ledger. AfterAll filters scratchDirs by this
		// prefix instead of maintaining a parallel ledger — the helper
		// registers each dir at claim time (before content write), so a
		// content-write failure mid-helper still leaves a registered
		// entry for cleanup to consume. The sibling #200 Context's
		// AfterAll only iterates scratchDirs entries that were present
		// when it ran, so #218 entries (added after) are still here.
		const repoScratchDirPrefix = "/tmp/tomei-system-package-repository"

		// readState parses ~/.local/share/tomei/system/state.json via shell
		// cat (state.json lives inside the container, not on the host where
		// the Go test process runs — os.ReadFile would fail).
		readState := func() state.SystemState {
			out, err := testExec.ExecBash("cat -- " + stateJSONPath)
			Expect(err).NotTo(HaveOccurred(), "reading %s failed: %s", stateJSONPath, out)
			var st state.SystemState
			Expect(json.Unmarshal([]byte(out), &st)).To(Succeed(),
				"state.json is not valid JSON; contents:\n%s", out)
			return st
		}

		// zeroTimestamps strips the per-apply UpdatedAt field so the
		// idempotency spec can compare structural equality. The map value
		// type is `*resource.SystemPackageRepositoryState` (pointer), so
		// direct mutation suffices — no reassignment to the map is needed.
		zeroTimestamps := func(s *state.SystemState) {
			for _, v := range s.SystemPackageRepositories {
				v.UpdatedAt = time.Time{}
			}
		}

		BeforeAll(func() {
			skipIfNotLinux()

			// gpg is NO LONGER required by apt.PackageRepositoryInstaller:
			// the key is dearmored in-process now (#283), so apply works on
			// minimal images without gnupg. gpg is still needed here purely
			// as the TEST ORACLE that validates the decoded keyring via
			// `gpg --list-keys` (the "lists ... public key" spec below) —
			// the strongest proof that the in-process decode byte-matches
			// `gpg --dearmor`.
			//
			// In TOMEI_E2E_CONTAINER mode the Dockerfile installs gnupg for
			// that oracle (see e2e/containers/ubuntu/Dockerfile). A
			// regression that drops the gnupg apt-get line must FAIL CI
			// rather than turn the keyring-validation coverage into a silent
			// skip, so the missing-gpg branch only Skips for native opt-in
			// runs and Expects in container mode.
			_, gpgErr := testExec.ExecBash("command -v gpg >/dev/null 2>&1")
			if gpgErr != nil {
				if os.Getenv("TOMEI_E2E_CONTAINER") != "" {
					Expect(gpgErr).NotTo(HaveOccurred(),
						"gpg missing in TOMEI_E2E_CONTAINER mode: the runner image installs gnupg via e2e/containers/ubuntu/Dockerfile as the keyring-validation oracle — a regression there silently turned that coverage into a skip")
				}
				Skip("gpg not found on PATH: the keyring-validation oracle requires gnupg (apply itself no longer needs it, #283); install `gnupg` or use the container e2e runner")
			}

			// NOPASSWD probe — same rationale as the #200 BeforeAll.
			_, sudoErr := testExec.ExecBash("sudo -k && sudo -n true 2>&1")
			Expect(sudoErr).NotTo(HaveOccurred(),
				"non-interactive sudo (NOPASSWD) is required: apply --system invokes apt under sudo")

			// Refuse to run on a host that already has pgdg configured.
			// Skipping is simpler and safer than snapshot-and-restore:
			// spec 1's install overwrites the live keyring/sources.list,
			// so any failure between overwrite and restore would leave
			// the host worse off than just skipping. The container e2e
			// runner always starts from a clean image, so this gate only
			// fires on native opt-in runs (or a reused container with
			// leftover pgdg files from a botched previous run).
			// Use pathExistsAny (existence-only, never hashes) so a
			// pre-existing unreadable root-owned PGDG file gets us a
			// clean Skip instead of an Expect failure inside the hash
			// step of fileSha256IfExists.
			for _, p := range []string{pgdgKeyringPath, pgdgSourcePath} {
				if pathExistsAny(p) {
					Skip(fmt.Sprintf("pre-existing %s on host: refusing to clobber a real PGDG setup (re-run this Context in the container e2e runner instead)", p))
				}
			}

			// `tomei init --force` is idempotent across Contexts.
			initOut, initErr := testExec.Exec("tomei", "init", "--yes", "--force")
			Expect(initErr).NotTo(HaveOccurred(), "tomei init failed before preflight: %s", initOut)

			// Gate the AfterAll cleanup ASAP — partial-failure paths past
			// this point still need the cleanup branch.
			pgdgPreflightComplete = true

			resetSystemState()

			scratchSuffix := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
			repoCfgPath = "/tmp/tomei-system-package-repository-" + scratchSuffix + "/"
			installerOnlyCfgPath = "/tmp/tomei-system-package-repository-removal-" + scratchSuffix + "/"
			// The helper registers each dir into the suite-level
			// scratchDirs ledger at claim time (right after the marker
			// touch, before the content write). So even if the second
			// call's content write aborts BeforeAll, AfterAll's prefix-
			// filtered cleanup will still find and remove the partial
			// dir — no separate per-Context ledger needed.
			writeSystemPackageRepositoryManifest(repoCfgPath, true)
			writeSystemPackageRepositoryManifest(installerOnlyCfgPath, false)
		})

		AfterAll(func() {
			if !pgdgPreflightComplete {
				return
			}
			// Honor the same native-mode escape hatch the sibling #200
			// AfterAll uses: a native opt-in run that
			// sets TOMEI_E2E_NATIVE_SKIP_CLEANUP=true to inspect host
			// state after a failure must apply consistently here too,
			// or this Context would silently re-clean what the sibling
			// preserved.
			if os.Getenv("TOMEI_E2E_NATIVE_SKIP_CLEANUP") == "true" {
				fmt.Fprintln(GinkgoWriter, "TOMEI_E2E_NATIVE_SKIP_CLEANUP=true: skipping #218 PGDG file + /tmp cleanup")
				return
			}
			// Scratch-dir cleanup: filter the suite-level scratchDirs
			// ledger by the #218 repo prefix. The sibling #200
			// AfterAll's cleanup loop has already run, but it didn't
			// delete map entries — and the helper registers our dirs at
			// claim time (right after marker touch), so a partial
			// content-write failure inside writeSystemPackageRepository-
			// Manifest still leaves an entry for us to remove.
			for dir := range scratchDirs {
				if !strings.HasPrefix(dir, repoScratchDirPrefix) {
					continue
				}
				if !systemPackageTestDirRE.MatchString(dir) {
					continue
				}
				_, markerErr := testExec.ExecBash(`test -f ` + dir + `/.tomei-e2e-system-package-test`)
				if markerErr != nil {
					continue
				}
				_, _ = testExec.ExecBash("rm -rf " + dir)
			}
			// pgdgMutationStarted gates the host cleanup — if no spec
			// ever invoked apply --system, the keyring/sources.list
			// files were never created by this suite. The BeforeAll
			// Skip guarantees both paths were absent pre-test, so any
			// surviving file here was written by this Context and is
			// safe to remove unconditionally.
			if !pgdgMutationStarted {
				return
			}
			removedAny := false
			for _, path := range []string{pgdgKeyringPath, pgdgSourcePath} {
				// Best-effort: tolerate non-zero exits so cleanup
				// failures never mask the spec-failure that surfaced
				// the real bug.
				_, _ = testExec.ExecBash("sudo -n rm -f -- " + path)
				removedAny = true
			}
			// If we touched apt sources, flush the apt cache so subsequent
			// apt operations in the same container don't 404 trying to
			// fetch from the now-removed pgdg source. Best-effort: failure
			// here would only manifest as a confusing apt error later, not
			// as a masked test-failure now. apt-get update uses APT::Lock
			// (not DPkg::Lock), so no Lock::Timeout override is meaningful.
			if removedAny {
				_, _ = testExec.ExecBash("sudo -n env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C apt-get update -y 2>&1 || true")
			}
			// Best-effort: bypass resetSystemState (which Expects) and
			// directly overwrite state.json, tolerating non-zero exits.
			// The rest of this AfterAll is best-effort so a cleanup-only
			// failure does not mask the spec failure that drove cleanup
			// here in the first place; the strict resetSystemState would
			// add an extra failure on a temporarily-unwritable path.
			_, _ = testExec.ExecBash(
				`mkdir -p ~/.local/share/tomei/system && echo '{"version":"1","systemInstallers":{},"systemPackageRepositories":{},"systemPackages":{}}' > ` + stateJSONPath + ` || true`)
		})

		It("apply --system installs the repository on the host",
			Label("needs-gnupg", "needs-network"), func() {
				// Irreversible gate: once set, the AfterAll cleanup
				// (above) is mandatory because some host mutation MAY
				// have already happened, even if ExecApply below
				// returns non-zero. Best-effort rm -f means partial
				// cleanup never cascades.
				pgdgMutationStarted = true
				out, err := ExecApply(testExec, "--system", repoCfgPath)
				Expect(err).NotTo(HaveOccurred(), "apply --system failed; output:\n%s", out)
				Expect(out).NotTo(ContainSubstring("system resource apply failed"),
					"apply --system surfaced a system-engine error as a warning; got:\n%s", out)

				By("keyring + sources.list files exist with correct mode/ownership", func() {
					_, err := testExec.ExecBash("test -f " + pgdgKeyringPath)
					Expect(err).NotTo(HaveOccurred(), "missing %s", pgdgKeyringPath)
					_, err = testExec.ExecBash("test -f " + pgdgSourcePath)
					Expect(err).NotTo(HaveOccurred(), "missing %s", pgdgSourcePath)
					_, err = testExec.ExecBash("test -s " + pgdgKeyringPath)
					Expect(err).NotTo(HaveOccurred(), "%s is empty", pgdgKeyringPath)
					// Exact-equality (not ContainSubstring): GNU stat
					// renders the special bits as a leading digit
					// (`1644 root:root` for sticky, `2644 root:root`
					// for setgid). ContainSubstring("644 root:root")
					// would pass for those, but the 0644 contract
					// (install -D -m 0644 -o root -g root) means a
					// regression that flips a special bit must fail.
					modeOut, err := testExec.ExecBash("stat -c '%a %U:%G' -- " + pgdgKeyringPath)
					Expect(err).NotTo(HaveOccurred(), "stat on %s failed", pgdgKeyringPath)
					Expect(strings.TrimSpace(modeOut)).To(Equal("644 root:root"),
						"keyring mode/ownership mismatch; stat: %s", strings.TrimSpace(modeOut))
					// sources.list is also installed via `install -D -m
					// 0644 -o root -g root` (see
					// internal/installer/apt/repository.go) — assert the
					// same so a regression that drops the install flags
					// for the sources.list path is caught.
					srcModeOut, err := testExec.ExecBash("stat -c '%a %U:%G' -- " + pgdgSourcePath)
					Expect(err).NotTo(HaveOccurred(), "stat on %s failed", pgdgSourcePath)
					Expect(strings.TrimSpace(srcModeOut)).To(Equal("644 root:root"),
						"sources.list mode/ownership mismatch; stat: %s", strings.TrimSpace(srcModeOut))
					// Validate keyring is a real GPG keyring. Uses --with-colons
					// machine-readable output (stable across gpg 2.x versions,
					// no locale dependency) so `^pub:` matches predictably.
					// Plain `--list-keys` text format varies (`pub `, `pub:`,
					// indented variants) across gpg versions.
					//
					// stderr is merged into stdout (2>&1) rather than dropped
					// to /dev/null so a real gpg failure (corrupt keyring,
					// missing agent socket) surfaces in test logs instead of
					// being silenced — pipefail propagates the non-zero exit.
					// Isolate this validation-oracle gpg from the host's
					// GNUPGHOME / gpg.conf: `--homedir` to a fresh /tmp
					// directory, `--no-options` so a developer's gpg.conf
					// can't change command semantics, and `--batch` to
					// prevent any pinentry/agent prompt. (The installer no
					// longer runs gpg at all — it dearmors in-process,
					// #283; this gpg invocation is purely the test oracle
					// proving the in-process-decoded keyring is a valid,
					// parseable OpenPGP keyring.) trap-cleanup removes the
					// homedir on success or failure so a leaked
					// /tmp/gpghome-* does not pile up.
					gpgOut, err := testExec.ExecBash(
						"set -o pipefail; H=$(mktemp -d -t tomei-e2e-gpg-XXXXXX); trap 'rm -rf -- \"$H\"' EXIT; gpg --homedir \"$H\" --no-options --batch --no-default-keyring --keyring " + pgdgKeyringPath + " --with-colons --list-keys 2>&1")
					Expect(err).NotTo(HaveOccurred(),
						"gpg --list-keys on %s failed; output:\n%s", pgdgKeyringPath, gpgOut)
					Expect(gpgOut).To(MatchRegexp(`(?m)^pub:`),
						"%s does not list any public key via gpg --with-colons; output:\n%s", pgdgKeyringPath, gpgOut)
					// A corrupt dearmor that produces a keyring with a pub
					// record but no associated user-id is hard to surface
					// downstream — apt-get update would fetch it and only
					// complain at first package install with a vague
					// "NO_PUBKEY" message. Pin the uid presence here.
					Expect(gpgOut).To(MatchRegexp(`(?m)^uid:`),
						"%s lists pub but no uid — keyring appears corrupt; output:\n%s", pgdgKeyringPath, gpgOut)
				})

				By("sources.list references the keyring + suite + url", func() {
					src, err := testExec.ExecBash("cat -- " + pgdgSourcePath)
					Expect(err).NotTo(HaveOccurred(), "cat %s failed", pgdgSourcePath)
					Expect(src).To(ContainSubstring("signed-by=" + pgdgKeyringPath))
					Expect(src).To(ContainSubstring(pgdgURL))
					Expect(src).To(ContainSubstring(pgdgSuite + " " + pgdgComponent))
				})

				By("state.json records the repository", func() {
					st := readState()
					Expect(st.SystemPackageRepositories).To(HaveKey("pgdg"))
					pgdg := st.SystemPackageRepositories["pgdg"]
					Expect(pgdg.InstallerRef).To(Equal("apt"))
					Expect(pgdg.Apt).NotTo(BeNil(), "state entry missing Apt details")
					Expect(pgdg.Apt.KeyHash).To(Equal(pgdgKeyHashSHA256))
					Expect(pgdg.Apt.Suite).To(Equal(pgdgSuite))
					// InstalledFiles order is deterministic per
					// internal/installer/apt/repository.go — keyring first,
					// sources.list second. The Remove path uses a separate
					// orderedPaths list, in reverse.
					Expect(pgdg.InstalledFiles).To(Equal([]string{pgdgKeyringPath, pgdgSourcePath}))
					Expect(pgdg.UpdatedAt).To(BeTemporally("~", time.Now(), 60*time.Second),
						"UpdatedAt must be set to apply time, not zero or stale")
				})

				// Snapshot the post-install file bytes so the no-flag
				// retention spec (#169 scenario 7) can prove the files
				// are not silently rewritten. Capturing here avoids
				// re-running spec 1 from the retention spec just to
				// know the "expected" content.
				postInstallFileHashes = map[string]string{}
				for _, p := range []string{pgdgKeyringPath, pgdgSourcePath} {
					h, exists := fileSha256IfExists(p)
					Expect(exists).To(BeTrue(), "post-install snapshot: %s should exist", p)
					postInstallFileHashes[p] = h
				}
			})

		// A truly idempotent second apply does NOT enter Install at
		// all — the pre-apply plan-zero-work guard below proves the
		// reconciler agrees there's nothing to do, and the post-apply
		// sha256 check proves no rewrite happened. If a second apply
		// reaches apt.postgresql.org / apt-get update at all, that is
		// the regression this spec catches, NOT a network flake to
		// dismiss. The `needs-network` label only documents that the
		// upstream install spec (which this depends on via Ordered)
		// hits the network — not that this spec is allowed to.
		It("apply --system twice is idempotent",
			Label("needs-gnupg", "needs-network"), func() {
				before := readState()
				zeroTimestamps(&before)

				// PRE-apply plan must already show zero work — this
				// catches a regression where the second apply WOULD
				// re-run install (rewriting bytes to the same content)
				// even though the post-apply structural state-equality
				// below would still pass. Without this guard, an
				// unnecessary re-install would only surface as a flaky
				// network-traffic spike.
				preplanOut, err := testExec.Exec("tomei", "plan", "--system", repoCfgPath)
				Expect(err).NotTo(HaveOccurred(), "pre-apply plan failed: %s", preplanOut)
				Expect(preplanOut).To(MatchRegexp(`(?m)^Summary:\s+0 to install,\s+0 to upgrade,\s+0 to reinstall,\s+0 to remove\s*$`),
					"pre-second-apply plan must already show zero work; got:\n%s", preplanOut)

				// Byte + mtime snapshot of the on-disk files going
				// into the second apply. After the apply we re-check
				// both: a same-content rewrite (e.g. `install -D`
				// run again with identical bytes) leaves the sha256
				// unchanged but advances mtime/ctime, so we need
				// both legs to actually catch an unnecessary
				// re-install.
				preApplyFileHashes := map[string]string{}
				preApplyFileIdents := map[string]string{}
				for _, p := range []string{pgdgKeyringPath, pgdgSourcePath} {
					h, exists := fileSha256IfExists(p)
					Expect(exists).To(BeTrue(), "pre-apply: %s missing", p)
					preApplyFileHashes[p] = h
					// Identify the file by inode + full-precision
					// mtime (`%y` includes nanoseconds). The installer
					// uses `install -D` which writes to a tmpfile and
					// renames into place, producing a NEW inode on
					// every run — so an inode change is a definitive
					// "install path re-ran" signal even when the
					// rewrite happens within the same wall-clock
					// second (whole-second `%Y` would miss that). The
					// nanosecond mtime is a redundant second leg in
					// case a filesystem (e.g. tmpfs without strictatime)
					// somehow recycles inodes.
					identOut, err := testExec.ExecBash("stat -c '%i|%y' -- " + p)
					Expect(err).NotTo(HaveOccurred(), "pre-apply: stat on %s failed", p)
					preApplyFileIdents[p] = strings.TrimSpace(identOut)
				}

				out, err := ExecApply(testExec, "--system", repoCfgPath)
				Expect(err).NotTo(HaveOccurred(), "second apply failed; output:\n%s", out)
				Expect(out).NotTo(ContainSubstring("system resource apply failed"),
					"idempotent re-apply surfaced system-engine warning; got:\n%s", out)

				for _, p := range []string{pgdgKeyringPath, pgdgSourcePath} {
					h, exists := fileSha256IfExists(p)
					Expect(exists).To(BeTrue(), "post-second-apply: %s missing", p)
					Expect(h).To(Equal(preApplyFileHashes[p]),
						"%s contents changed across idempotent re-apply (sha256 mismatch); the install path re-ran when it should have no-op'd", p)
					identOut, err := testExec.ExecBash("stat -c '%i|%y' -- " + p)
					Expect(err).NotTo(HaveOccurred(), "post-apply: stat on %s failed", p)
					Expect(strings.TrimSpace(identOut)).To(Equal(preApplyFileIdents[p]),
						"%s inode or mtime changed across idempotent re-apply; the install path re-wrote identical bytes (likely via install -D) when it should have no-op'd", p)
				}

				after := readState()
				zeroTimestamps(&after)
				Expect(after).To(Equal(before),
					"idempotent re-apply mutated state.json beyond UpdatedAt")

				// Explicit slice-order pin: structural Equal above already
				// covers this transitively, but a future regression that
				// shuffles InstalledFiles non-deterministically (e.g. via
				// map iteration) is easier to diagnose when the failure
				// names the slice rather than reading as "state differs".
				// HaveKey guard prevents a nil-pointer deref if a separate
				// regression made spec 1's install silently no-op.
				Expect(after.SystemPackageRepositories).To(HaveKey("pgdg"))
				Expect(after.SystemPackageRepositories["pgdg"].InstalledFiles).To(
					Equal([]string{pgdgKeyringPath, pgdgSourcePath}),
					"InstalledFiles order regressed after idempotent re-apply")

				// Second layer of idempotency: a fresh plan must show no
				// pending changes. Catches the case where state-write was
				// correctly skipped but the engine still queued install
				// actions that just no-op'd.
				planOut, err := testExec.Exec("tomei", "plan", "--system", repoCfgPath)
				Expect(err).NotTo(HaveOccurred(), "plan --system failed: %s", planOut)
				Expect(planOut).To(MatchRegexp(`(?m)^Summary:\s+0 to install,\s+0 to upgrade,\s+0 to reinstall,\s+0 to remove\s*$`),
					"post-apply plan must show zero pending actions; got:\n%s", planOut)
			})

		It("apply WITHOUT --system skips system resources and retains state (#169 scenario 7)",
			// needs-network: this spec reads state populated by the
			// upstream install spec in the same Ordered Context. A
			// label filter that excludes network tests must therefore
			// exclude this one too, or the prior install will not run
			// and `before.SystemPackageRepositories` is empty.
			Label("needs-gnupg", "needs-network"), func() {
				// Spec intent: even though installerOnlyCfgPath drops
				// pgdgRepo from the manifest, omitting --system makes
				// apply ignore system resources entirely — state.json and
				// on-disk files must stay untouched.
				//
				// Scope: filesystem drift between applies (manual keyring
				// deletion by a user) is intentionally out of scope here
				// and deferred to a future audit feature.
				before := readState()
				Expect(before.SystemPackageRepositories).To(HaveKey("pgdg"))
				Expect(postInstallFileHashes).NotTo(BeEmpty(),
					"postInstallFileHashes must have been captured by the upstream install spec")

				out, err := ExecApply(testExec, installerOnlyCfgPath) // NOTE: no --system
				Expect(err).NotTo(HaveOccurred(), "apply (no --system) failed: %s", out)
				Expect(out).NotTo(ContainSubstring("system resource apply failed"))
				// User-visible warning from cmd/tomei/apply.go.
				// NOTE: this exact string is UX-coupling — if the wording
				// in apply.go changes, update both in lockstep.
				Expect(out).To(ContainSubstring("system resource(s) skipped. Use 'tomei apply --system' or 'tomei apply --system-only' to manage"),
					"expected the skip warning to be printed; got:\n%s", out)

				// Files and state must be byte-identical to the
				// post-install snapshot. Existence-only checks would
				// miss a regression that rewrites the keyring or
				// sources.list while keeping the path.
				for _, p := range []string{pgdgKeyringPath, pgdgSourcePath} {
					h, exists := fileSha256IfExists(p)
					Expect(exists).To(BeTrue(), "%s missing after no-flag apply", p)
					Expect(h).To(Equal(postInstallFileHashes[p]),
						"%s contents changed after no-flag apply (sha256 mismatch); the no-flag path must not touch system files", p)
				}
				after := readState()
				Expect(after.SystemPackageRepositories).To(HaveKey("pgdg"))
				// Structural state equality. Same rationale as files:
				// a no-flag apply must not perturb state.json either.
				Expect(after).To(Equal(before),
					"state.json changed after no-flag apply; system resources must not be reconciled")
			})

		It("removing the repo from the manifest cleans up files and state",
			// needs-network: depends on the upstream install spec to
			// have populated state.json with pgdg. Without it, the
			// reconciler would have nothing to remove and the test
			// would fail for setup reasons rather than testing removal.
			Label("needs-gnupg", "needs-network"), func() {
				// Spec intent: same installerOnlyCfgPath manifest as the
				// scenario-7 spec above, but WITH --system this time. The
				// engine reconciler treats pgdg missing-from-manifest +
				// present-in-state as an implicit remove signal.
				out, err := ExecApply(testExec, "--system", installerOnlyCfgPath)
				Expect(err).NotTo(HaveOccurred(), "removal apply failed: %s", out)
				Expect(out).NotTo(ContainSubstring("system resource apply failed"))

				for _, p := range []string{pgdgKeyringPath, pgdgSourcePath} {
					// Removal contract: NO filesystem entry of any kind
					// at this path. Both `-e` and `-L` are required —
					// `-e` follows symlinks (so a dangling symlink
					// passes `! -e`), and `-L` checks the link itself
					// without following. Pairing them rejects regular
					// files, directories, dangling symlinks, and live
					// symlinks alike.
					_, err := testExec.ExecBash("test ! -e " + p + " && test ! -L " + p)
					Expect(err).NotTo(HaveOccurred(), "%s should have been removed (file, directory, or symlink left behind)", p)
				}
				st := readState()
				Expect(st.SystemPackageRepositories).NotTo(HaveKey("pgdg"),
					"state.json must no longer reference pgdg after removal apply")
			})
	})
}
