//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/terassyi/tomei/internal/state"
)

// privilegedDownloadTests covers SUB8 (#231): end-to-end placement of a
// privileged download-pattern tool's symlink under /usr/local/bin via the
// SUB3 portable helper (sudo -n ln fallback). Pairs with the existing
// Commands-pattern coverage in privileged_test.go.
//
// Container-only (D1). /usr/local/bin is host-global; native opt-in would
// mutate the operator's host. The Commands test in privileged_test.go does
// run natively because it writes to /tmp; this one cannot.
//
// Download pattern only (D2). installFromRegistry delegates to
// installByDownload via buildResolvedTool (which preserves Privileged); the
// system-bin routing branch is shared. Doubling the fixture adds aqua-registry
// network dependence without doubling coverage.
//
// Reuses the gh URL/version/checksum the basic e2e already trusts (D3) and
// materializes the manifest via heredoc inside the container HOME (D7 — no
// Dockerfile changes).
func privilegedDownloadTests() {
	const (
		ghLinkPath = "/usr/local/bin/gh"
		ghVersion  = "2.86.0" // mirrors e2e/config/manifests/tools.cue (_ghVersion)
	)

	var (
		preflightComplete   bool
		fullManifestPath    string
		reducedManifestPath string
		scratchDir          string
		// Stat snapshot ("inode|mtime") captured after phase 1's install,
		// asserted unchanged across the idempotent re-apply (phase 2) and the
		// no-flag apply (phase 3). Declared at Context scope so Ginkgo Ordered
		// semantics carry the value across phases.
		postInstallStat string
	)

	// readUserState parses ~/.local/share/tomei/state.json via shell cat
	// (state.json lives inside the container; os.ReadFile on the host side
	// where the Go test process runs would fail). Mirrors readState in
	// system_package_test.go:1758.
	readUserState := func() *state.UserState {
		out, err := testExec.ExecBash("cat -- ~/.local/share/tomei/state.json")
		Expect(err).NotTo(HaveOccurred(), "reading user state.json failed: %s", out)
		var st state.UserState
		Expect(json.Unmarshal([]byte(out), &st)).To(Succeed(),
			"user state.json is not valid JSON; contents:\n%s", out)
		return &st
	}

	// zeroToolTimestamps strips the per-apply UpdatedAt field on every Tools
	// entry so two snapshots can be compared structurally with Equal across
	// an idempotent or no-op apply. Mirrors zeroTimestamps in
	// system_package_test.go:1771. The Tools map value type is ToolState by
	// value, so reassign back into the map after mutation.
	zeroToolTimestamps := func(s *state.UserState) {
		for k, v := range s.Tools {
			v.UpdatedAt = time.Time{}
			s.Tools[k] = v
		}
	}

	// materializeManifests writes the full + reduced CUE manifests into the
	// scratch dir inside the container HOME. Mirrors the heredoc pattern in
	// system_package_test.go's writeSystemPackageRepositoryManifest. The full
	// manifest carries one privileged download Tool (gh); the reduced one
	// drops the Tool entry — used in phase 4 to trigger removal.
	materializeManifests := func() (string, string, string) {
		// Tilde expansion: bash expands ~ in unquoted contexts (mkdir, cat
		// redirects) and tomei expands ~ on its CLI manifest arg.
		// Bash's $HOME, by contrast, only expands shell-side — passing
		// "$HOME/foo" to `tomei apply` would stat the literal "$HOME/..."
		// path and fail.
		dir := fmt.Sprintf("~/privileged-download-test-%d-%d",
			os.Getpid(), time.Now().UnixNano())
		setup := fmt.Sprintf(`set -euo pipefail
mkdir -p %[1]s/cue.mod
cat > %[1]s/cue.mod/module.cue <<'EOF'
module: "tomei.local@v0"
language: version: "v0.9.0"
EOF
`, dir)
		_, err := testExec.ExecBash(setup)
		Expect(err).NotTo(HaveOccurred(), "scratch-dir setup failed")

		// Full manifest: one privileged download Tool. URL pattern mirrors
		// e2e/config/manifests/tools.cue (gh release archive) so we reuse a
		// proven-in-CI URL/checksum pair. CI matrix runs both linux/amd64
		// and linux/arm64 containers (ci.yaml); read runtime.GOARCH so the
		// URL tracks the host arch. The gh release ships both arches under
		// a shared checksums.txt, so only the URL needs the arch swap.
		fullManifest := fmt.Sprintf(`set -euo pipefail
cat > %[1]s/full.cue <<'EOF'
package tomei

ghPrivileged: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "gh"
	spec: {
		installerRef: "download"
		version:      "%[2]s"
		privileged:   true
		source: {
			url: "https://github.com/cli/cli/releases/download/v%[2]s/gh_%[2]s_linux_%[3]s.tar.gz"
			checksum: url: "https://github.com/cli/cli/releases/download/v%[2]s/gh_%[2]s_checksums.txt"
		}
	}
}
EOF
`, dir, ghVersion, runtime.GOARCH)
		_, err = testExec.ExecBash(fullManifest)
		Expect(err).NotTo(HaveOccurred(), "writing full manifest failed")

		// Reduced manifest: no Tool entries. The loader treats an empty
		// "package tomei" as "no resources", which lets tomei plan compute
		// a Remove for the gh entry already in state.
		reducedManifest := fmt.Sprintf(`set -euo pipefail
mkdir -p %[1]s/reduced/cue.mod
cp %[1]s/cue.mod/module.cue %[1]s/reduced/cue.mod/
cat > %[1]s/reduced/reduced.cue <<'EOF'
package tomei
EOF
`, dir)
		_, err = testExec.ExecBash(reducedManifest)
		Expect(err).NotTo(HaveOccurred(), "writing reduced manifest failed")

		return dir + "/full.cue", dir + "/reduced", dir
	}

	Context("Apply --system installs privileged download tool (#231)", Ordered, func() {
		BeforeAll(func() {
			// D1: container-only gate. /usr/local/bin is host-global; native
			// opt-in could clobber operator-installed binaries. Different
			// from privileged_test.go's no-gate policy because that test
			// writes to /tmp; this one writes to /usr/local/bin.
			if os.Getenv("TOMEI_E2E_CONTAINER") == "" {
				Skip("privileged download e2e requires TOMEI_E2E_CONTAINER; native mode would mutate /usr/local/bin on the host")
			}

			// NOPASSWD probe — the SUB3 sudo fallback path is non-interactive.
			// In the Dockerfile-managed container this is set up; surface a
			// clear error if a future Dockerfile regression removes it.
			out, err := testExec.ExecBash("sudo -n true")
			Expect(err).NotTo(HaveOccurred(), "container must provide NOPASSWD sudo: %s", out)

			// Pre-existence guard: refuse to mutate if /usr/local/bin/gh
			// already exists. Would poison the install assertion and the
			// cleanup decision. The container's Dockerfile installs
			// /usr/local/bin/tomei but never gh, so this is normally clean.
			out, err = testExec.ExecBash("test ! -e " + ghLinkPath)
			if err != nil {
				Skip(ghLinkPath + " already exists; preflight refuses to mutate it. out: " + out)
			}

			// Fresh state. `--yes` skips the interactive confirm prompt for
			// `tomei init --force` (mirrors privileged_test.go:16).
			_, err = testExec.Exec("tomei", "init", "--yes", "--force")
			Expect(err).NotTo(HaveOccurred())
			_, err = testExec.ExecBash(`echo '{"runtimes":{},"tools":{},"installers":{},"installerRepositories":{}}' > ~/.local/share/tomei/state.json`)
			Expect(err).NotTo(HaveOccurred(), "user state reset failed")

			fullManifestPath, reducedManifestPath, scratchDir = materializeManifests()
			preflightComplete = true
		})

		AfterAll(func() {
			if !preflightComplete {
				return
			}
			// Escape hatch shared with the rest of the e2e suite (see
			// system_package_test.go:992, 1858) — strict "true" check, NOT
			// non-empty, so TOMEI_E2E_NATIVE_SKIP_CLEANUP=false (or any other
			// non-empty value) does NOT silently disable cleanup here while
			// running it in the sibling Contexts. Container runs are
			// ephemeral so this rarely matters, but keep the symmetry.
			if os.Getenv("TOMEI_E2E_NATIVE_SKIP_CLEANUP") == "true" {
				return
			}
			// Best-effort: the symlink may already be gone after phase 4's
			// removal It; sudo -n rm -f is idempotent on missing paths.
			_, _ = testExec.ExecBash("sudo -n rm -f " + ghLinkPath)
			// Reset user state so subsequent suites start clean.
			_, _ = testExec.ExecBash(`echo '{"runtimes":{},"tools":{},"installers":{},"installerRepositories":{}}' > ~/.local/share/tomei/state.json`)
			if scratchDir != "" {
				_, _ = testExec.ExecBash("rm -rf " + scratchDir)
			}
		})

		It("apply --system installs the symlink at /usr/local/bin/gh", func() {
			output, err := ExecApply(testExec, "--system", fullManifestPath)
			Expect(err).NotTo(HaveOccurred(), "apply --system failed; output:\n%s", output)

			By("symlink exists and points into ~/.local/share/tomei/tools/gh/", func() {
				out, err := testExec.ExecBash("test -L " + ghLinkPath + " && readlink " + ghLinkPath)
				Expect(err).NotTo(HaveOccurred(), "readlink failed: %s", out)
				Expect(out).To(ContainSubstring("/.local/share/tomei/tools/gh/"),
					"symlink must point into the user data dir, not /usr/local/...")
			})

			By("the binary at the symlink target is user-owned (not root)", func() {
				// `stat -c '%U'` resolves the symlink (no -L = -L by default
				// for stat in coreutils; use --dereference explicitly to be
				// safe across alternate stats).
				out, err := testExec.ExecBash("stat -c '%U' --dereference " + ghLinkPath)
				Expect(err).NotTo(HaveOccurred(), "stat failed: %s", out)
				Expect(strings.TrimSpace(out)).To(Equal("testuser"),
					"the binary lives under ~/.local/share/tomei/tools/ — must be user-owned regardless of where its symlink points")
			})

			By("state.json carries BinDirKind=system, Privileged=true, BinPath under /usr/local/bin", func() {
				st := readUserState()
				tool, ok := st.Tools["gh"]
				Expect(ok).To(BeTrue(), "state.Tools must include gh; got: %+v", st.Tools)
				Expect(string(tool.BinDirKind)).To(Equal("system"),
					"SUB5 routes privileged download installs through systemBinDir; BinDirKind must persist as \"system\"")
				Expect(tool.Privileged).To(BeTrue(),
					"SUB7 persists spec.Privileged on the download/registry write path")
				Expect(tool.BinPath).To(Equal(ghLinkPath))
				Expect(tool.InstallPath).To(ContainSubstring("/.local/share/tomei/tools/gh/"),
					"the binary itself lives under the user data dir; only the symlink is system-bin")
			})

			// Snapshot the inode + nanosecond mtime for the idempotency
			// assertion in phase 2.
			out, err := testExec.ExecBash("stat -c '%i|%y' " + ghLinkPath)
			Expect(err).NotTo(HaveOccurred(), "stat snapshot failed: %s", out)
			postInstallStat = strings.TrimSpace(out)
		})

		It("apply --system is idempotent on the second run", func() {
			By("plan reports zero work (tight regex pin — substring would let '10 to install' pass)", func() {
				out, err := testExec.Exec("tomei", "plan", "--system", fullManifestPath)
				Expect(err).NotTo(HaveOccurred(), "plan --system failed: %s", out)
				// Format pinned at internal/graph/tree.go (Summary line);
				// identical pattern used by PR #218 at
				// system_package_test.go:1538, 2052.
				Expect(out).To(MatchRegexp(`(?m)^Summary:\s+0 to install,\s+0 to upgrade,\s+0 to reinstall,\s+0 to remove\s*$`),
					"idempotent plan must report zero work in every action bucket")
			})

			// before = post-phase-1 state under Ordered semantics.
			before := readUserState()
			_, err := ExecApply(testExec, "--system", fullManifestPath)
			Expect(err).NotTo(HaveOccurred(), "second apply --system failed")
			after := readUserState()

			By("symlink inode+mtime stable across the no-op apply", func() {
				out, err := testExec.ExecBash("stat -c '%i|%y' " + ghLinkPath)
				Expect(err).NotTo(HaveOccurred(), "stat after idempotent apply: %s", out)
				Expect(strings.TrimSpace(out)).To(Equal(postInstallStat),
					"idempotent re-apply must NOT re-create the symlink; engine should short-circuit on ActionNone before reaching createSymlink")
			})

			By("state.json structurally equal modulo UpdatedAt (PR #218 zeroTimestamps pattern)", func() {
				zeroToolTimestamps(before)
				zeroToolTimestamps(after)
				Expect(after).To(Equal(before),
					"idempotent re-apply must NOT rewrite state.json with refreshed content")
			})
		})

		It("apply WITHOUT --system leaves the privileged symlink untouched", func() {
			before := readUserState()
			output, err := ExecApply(testExec, fullManifestPath)
			Expect(err).NotTo(HaveOccurred(), "apply (no --system) failed; output:\n%s", output)
			Expect(output).To(ContainSubstring("privileged resource(s) skipped"),
				"SUB7's user-facing summary must mention the skip; otherwise the gate silently dropped the tool")

			By("symlink inode+mtime stable", func() {
				out, err := testExec.ExecBash("stat -c '%i|%y' " + ghLinkPath)
				Expect(err).NotTo(HaveOccurred(), "stat after no-flag apply: %s", out)
				Expect(strings.TrimSpace(out)).To(Equal(postInstallStat),
					"no-flag apply must NOT touch /usr/local/bin/gh — the gate filters before the install path runs")
			})

			By("state.Tools[gh] entry unchanged (no-flag apply is purely a filter, not a state write)", func() {
				after := readUserState()
				zeroToolTimestamps(before)
				zeroToolTimestamps(after)
				Expect(after).To(Equal(before))
			})
		})

		It("apply --system with the tool removed from manifest cleans up the symlink", func() {
			_, err := ExecApply(testExec, "--system", reducedManifestPath)
			Expect(err).NotTo(HaveOccurred())

			out, err := testExec.ExecBash("test ! -e " + ghLinkPath)
			Expect(err).NotTo(HaveOccurred(),
				ghLinkPath+" must be removed by the --system removal apply; got: %s", out)

			st := readUserState()
			_, present := st.Tools["gh"]
			Expect(present).To(BeFalse(),
				"state.Tools entry for gh must be deleted after removal apply")
		})
	})
}
