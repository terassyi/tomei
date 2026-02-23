//go:build e2e

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func registryTests() {

	BeforeAll(func() {
		By("Running tomei init to ensure state.json exists")
		_, err := testExec.Exec("tomei", "init", "--yes", "--force")
		Expect(err).NotTo(HaveOccurred())
	})

	Context("Registry Initialization", func() {
		It("init saves registry ref to state.json", func() {
			By("Reading state.json after init (from basic tests)")
			output, err := testExec.ExecBash("cat ~/.local/share/tomei/state.json")
			Expect(err).NotTo(HaveOccurred())

			By("Checking registry.aqua.ref exists")
			Expect(output).To(ContainSubstring(`"registry"`))
			Expect(output).To(ContainSubstring(`"aqua"`))
			Expect(output).To(ContainSubstring(`"ref"`))
		})

		It("registry ref is valid format", func() {
			By("Extracting registry ref from state.json")
			output, err := testExec.ExecBash("cat ~/.local/share/tomei/state.json | grep -o '\"ref\": \"v[0-9]*\\.[0-9]*\\.[0-9]*\"'")
			Expect(err).NotTo(HaveOccurred())

			By("Checking ref starts with v")
			Expect(output).To(MatchRegexp(`"ref": "v\d+\.\d+\.\d+"`))
		})
	})

	Context("Tool Installation - Aqua Registry", func() {
		It("validates registry manifests", func() {
			By("Running tomei validate on registry manifests")
			output, err := testExec.Exec("tomei", "validate", "~/manifests/registry/")
			Expect(err).NotTo(HaveOccurred())

			By("Checking validation succeeded")
			Expect(output).To(ContainSubstring("Validation successful"))

			By("Checking Tool/rg is recognized")
			Expect(output).To(ContainSubstring("Tool/rg"))

			By("Checking Tool/fd is recognized")
			Expect(output).To(ContainSubstring("Tool/fd"))

			By("Checking Tool/jq is recognized")
			Expect(output).To(ContainSubstring("Tool/jq"))
		})

		It("installs tools via aqua registry", func() {
			By("Running tomei apply on registry manifests")
			output, err := ExecApply(testExec, "~/manifests/registry/")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying apply completed with installations")
			Expect(output).To(ContainSubstring("Apply complete!"))
			Expect(output).To(ContainSubstring("Installed:"))
		})

		It("installs ripgrep via aqua registry", func() {
			By("Checking ripgrep binary exists")
			_, err := testExec.ExecBash("ls ~/.local/bin/rg")
			Expect(err).NotTo(HaveOccurred())

			By("Executing rg --version")
			output, err := testExec.ExecBash("~/.local/bin/rg --version")
			Expect(err).NotTo(HaveOccurred())

			By("Checking ripgrep version output")
			Expect(output).To(ContainSubstring("ripgrep " + versions.RegRgVersion))
		})

		It("installs fd via aqua registry", func() {
			By("Checking fd binary exists")
			_, err := testExec.ExecBash("ls ~/.local/bin/fd")
			Expect(err).NotTo(HaveOccurred())

			By("Executing fd --version")
			output, err := testExec.ExecBash("~/.local/bin/fd --version")
			Expect(err).NotTo(HaveOccurred())

			By("Checking fd version output")
			Expect(output).To(ContainSubstring("fd " + versions.RegFdVersion))
		})

		It("installs jq via aqua registry", func() {
			By("Checking jq binary exists")
			_, err := testExec.ExecBash("ls ~/.local/bin/jq")
			Expect(err).NotTo(HaveOccurred())

			By("Executing jq --version")
			output, err := testExec.ExecBash("~/.local/bin/jq --version")
			Expect(err).NotTo(HaveOccurred())

			By("Checking jq version output")
			Expect(output).To(ContainSubstring(versions.RegJqVersion))
		})

		It("OS/arch is correctly resolved", func() {
			By("Checking ripgrep binary architecture with file command (following symlink)")
			// Use readlink to get actual binary path, then check with file command
			output, err := testExec.ExecBash("file $(readlink -f ~/.local/bin/rg)")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying binary matches target architecture")
			// Check based on target architecture
			// Linux: "ARM aarch64" / "x86-64"
			// macOS: "Mach-O 64-bit executable arm64" / "Mach-O 64-bit executable x86_64"
			switch targetArch {
			case "arm64":
				Expect(output).To(SatisfyAny(
					ContainSubstring("ARM aarch64"),
					ContainSubstring("Mach-O 64-bit executable arm64"),
				))
			case "amd64":
				Expect(output).To(SatisfyAny(
					ContainSubstring("x86-64"),
					ContainSubstring("Mach-O 64-bit executable x86_64"),
				))
			default:
				Fail("Unsupported architecture: " + targetArch)
			}
		})

		It("records package in state.json", func() {
			By("Reading state.json")
			output, err := testExec.ExecBash("cat ~/.local/share/tomei/state.json")
			Expect(err).NotTo(HaveOccurred())

			By("Checking rg tool has package recorded")
			Expect(output).To(ContainSubstring(`"rg"`))
			Expect(output).To(ContainSubstring(`"owner": "BurntSushi"`))
			Expect(output).To(ContainSubstring(`"repo": "ripgrep"`))

			By("Checking fd tool has package recorded")
			Expect(output).To(ContainSubstring(`"fd"`))
			Expect(output).To(ContainSubstring(`"owner": "sharkdp"`))
			Expect(output).To(ContainSubstring(`"repo": "fd"`))

			By("Checking jq tool has package recorded")
			Expect(output).To(ContainSubstring(`"jq"`))
			Expect(output).To(ContainSubstring(`"owner": "jqlang"`))
			Expect(output).To(ContainSubstring(`"repo": "jq"`))
		})
	})

	Context("Registry Sync", func() {
		It("--sync flag works with apply", func() {
			By("Running tomei apply --sync")
			output, err := ExecApply(testExec, "--sync", "~/manifests/registry/")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying --sync apply completed without error")
			// --sync may or may not result in changes depending on registry state,
			// but the command should succeed without error
			Expect(output).To(SatisfyAny(
				ContainSubstring("No changes to apply"),
				ContainSubstring("Apply complete!"),
			))
		})
	})

	Context("Idempotency", func() {
		It("subsequent apply has no changes", func() {
			By("Running tomei apply again on registry manifests")
			output, err := ExecApply(testExec, "~/manifests/registry/")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying no changes were needed")
			Expect(output).To(ContainSubstring("No changes to apply"))
		})

		It("tools still work after idempotent apply", func() {
			By("Checking ripgrep still works")
			output, err := testExec.ExecBash("~/.local/bin/rg --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("ripgrep " + versions.RegRgVersion))

			By("Checking fd still works")
			output, err = testExec.ExecBash("~/.local/bin/fd --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("fd " + versions.RegFdVersion))

			By("Checking jq still works")
			output, err = testExec.ExecBash("~/.local/bin/jq --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring(versions.RegJqVersion))
		})
	})

	Context("Version Upgrade", func() {
		It("downgrades to older version", func() {
			By(fmt.Sprintf("Swapping manifest to older version (%s -> %s, etc)", versions.RegRgVersion, versions.RegRgVersionOld))
			_, err := testExec.ExecBash("mv ~/manifests/registry/tools.cue ~/manifests/registry/tools.cue.new")
			Expect(err).NotTo(HaveOccurred())
			_, err = testExec.ExecBash("mv ~/manifests/registry/tools.cue.old ~/manifests/registry/tools.cue")
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei apply with older version")
			_, err = ExecApply(testExec, "~/manifests/registry/")
			Expect(err).NotTo(HaveOccurred())
		})

		It("verifies older versions are installed", func() {
			By("Checking ripgrep downgraded to " + versions.RegRgVersionOld)
			output, err := testExec.ExecBash("~/.local/bin/rg --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("ripgrep " + versions.RegRgVersionOld))

			By("Checking fd downgraded to " + versions.RegFdVersionOld)
			output, err = testExec.ExecBash("~/.local/bin/fd --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("fd " + versions.RegFdVersionOld))

			By("Checking jq downgraded to " + versions.RegJqVersionOld)
			output, err = testExec.ExecBash("~/.local/bin/jq --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring(versions.RegJqVersionOld))
		})

		It("upgrades back to newer version", func() {
			By("Swapping manifest back to newer version")
			_, err := testExec.ExecBash("mv ~/manifests/registry/tools.cue ~/manifests/registry/tools.cue.old")
			Expect(err).NotTo(HaveOccurred())
			_, err = testExec.ExecBash("mv ~/manifests/registry/tools.cue.new ~/manifests/registry/tools.cue")
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei apply with newer version")
			_, err = ExecApply(testExec, "~/manifests/registry/")
			Expect(err).NotTo(HaveOccurred())
		})

		It("verifies newer versions are installed after upgrade", func() {
			By("Checking ripgrep upgraded to " + versions.RegRgVersion)
			output, err := testExec.ExecBash("~/.local/bin/rg --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("ripgrep " + versions.RegRgVersion))

			By("Checking fd upgraded to " + versions.RegFdVersion)
			output, err = testExec.ExecBash("~/.local/bin/fd --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("fd " + versions.RegFdVersion))

			By("Checking jq upgraded to " + versions.RegJqVersion)
			output, err = testExec.ExecBash("~/.local/bin/jq --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring(versions.RegJqVersion))
		})

		It("is idempotent after version changes", func() {
			By("Running tomei apply again")
			output, err := ExecApply(testExec, "~/manifests/registry/")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying no changes were needed")
			Expect(output).To(ContainSubstring("No changes to apply"))
		})
	})

	Context("Three-Segment Package", func() {
		BeforeAll(func() {
			// Re-init to get fresh state with aqua registry configured
			_, _ = testExec.Exec("tomei", "init", "--yes", "--force")
		})

		It("validates 3-segment package manifest", func() {
			By("Running tomei validate on three-segment logcli manifest")
			output, err := testExec.Exec("tomei", "validate", "~/three-segment-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Validation successful"))
			Expect(output).To(ContainSubstring("Tool/logcli"))
		})

		It("resolves 3-segment package via plan", func() {
			By("Running tomei plan — should resolve registry and show install action")
			output, err := testExec.Exec("tomei", "plan", "~/three-segment-test/")
			Expect(err).NotTo(HaveOccurred())

			By("Checking plan recognizes the tool")
			Expect(output).To(ContainSubstring("Tool/logcli"))
			Expect(output).To(ContainSubstring(versions.RegLogcliVersion))
			Expect(output).To(ContainSubstring("install"))
		})
	})
}
