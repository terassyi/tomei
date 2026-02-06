//go:build e2e

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func registryTests() {

	BeforeAll(func() {
		By("Running toto init to ensure state.json exists")
		_, err := testExec.Exec("toto", "init", "--yes", "--force")
		Expect(err).NotTo(HaveOccurred())
	})

	Context("Registry Initialization", func() {
		It("init saves registry ref to state.json", func() {
			By("Reading state.json after init (from basic tests)")
			output, err := testExec.ExecBash("cat ~/.local/share/toto/state.json")
			Expect(err).NotTo(HaveOccurred())

			By("Checking registry.aqua.ref exists")
			Expect(output).To(ContainSubstring(`"registry"`))
			Expect(output).To(ContainSubstring(`"aqua"`))
			Expect(output).To(ContainSubstring(`"ref"`))
		})

		It("registry ref is valid format", func() {
			By("Extracting registry ref from state.json")
			output, err := testExec.ExecBash("cat ~/.local/share/toto/state.json | grep -o '\"ref\": \"v[0-9]*\\.[0-9]*\\.[0-9]*\"'")
			Expect(err).NotTo(HaveOccurred())

			By("Checking ref starts with v")
			Expect(output).To(MatchRegexp(`"ref": "v\d+\.\d+\.\d+"`))
		})
	})

	Context("Tool Installation - Aqua Registry", func() {
		It("validates registry manifests", func() {
			By("Running toto validate on registry manifests")
			output, err := testExec.Exec("toto", "validate", "~/manifests/registry/")
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
			By("Running toto apply on registry manifests")
			_, err := testExec.Exec("toto", "apply", "~/manifests/registry/")
			Expect(err).NotTo(HaveOccurred())
		})

		It("installs ripgrep via aqua registry", func() {
			By("Checking ripgrep binary exists")
			_, err := testExec.ExecBash("ls ~/.local/bin/rg")
			Expect(err).NotTo(HaveOccurred())

			By("Executing rg --version")
			output, err := testExec.ExecBash("~/.local/bin/rg --version")
			Expect(err).NotTo(HaveOccurred())

			By("Checking ripgrep version output")
			Expect(output).To(ContainSubstring("ripgrep 15.1.0"))
		})

		It("installs fd via aqua registry", func() {
			By("Checking fd binary exists")
			_, err := testExec.ExecBash("ls ~/.local/bin/fd")
			Expect(err).NotTo(HaveOccurred())

			By("Executing fd --version")
			output, err := testExec.ExecBash("~/.local/bin/fd --version")
			Expect(err).NotTo(HaveOccurred())

			By("Checking fd version output")
			Expect(output).To(ContainSubstring("fd 10.3.0"))
		})

		It("installs jq via aqua registry", func() {
			By("Checking jq binary exists")
			_, err := testExec.ExecBash("ls ~/.local/bin/jq")
			Expect(err).NotTo(HaveOccurred())

			By("Executing jq --version")
			output, err := testExec.ExecBash("~/.local/bin/jq --version")
			Expect(err).NotTo(HaveOccurred())

			By("Checking jq version output")
			Expect(output).To(ContainSubstring("jq-1.8.1"))
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
			output, err := testExec.ExecBash("cat ~/.local/share/toto/state.json")
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
			By("Running toto apply --sync")
			_, err := testExec.Exec("toto", "apply", "--sync", "~/manifests/registry/")
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("Idempotency", func() {
		It("subsequent apply has no changes", func() {
			By("Running toto apply again on registry manifests")
			_, err := testExec.Exec("toto", "apply", "~/manifests/registry/")
			Expect(err).NotTo(HaveOccurred())
		})

		It("tools still work after idempotent apply", func() {
			By("Checking ripgrep still works")
			output, err := testExec.ExecBash("~/.local/bin/rg --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("ripgrep 15.1.0"))

			By("Checking fd still works")
			output, err = testExec.ExecBash("~/.local/bin/fd --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("fd 10.3.0"))

			By("Checking jq still works")
			output, err = testExec.ExecBash("~/.local/bin/jq --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("jq-1.8.1"))
		})
	})

	Context("Version Upgrade", func() {
		It("downgrades to older version", func() {
			By("Swapping manifest to older version (15.1.0 -> 14.1.1, etc)")
			_, err := testExec.ExecBash("mv ~/manifests/registry/tools.cue ~/manifests/registry/tools.cue.new")
			Expect(err).NotTo(HaveOccurred())
			_, err = testExec.ExecBash("mv ~/manifests/registry/tools.cue.old ~/manifests/registry/tools.cue")
			Expect(err).NotTo(HaveOccurred())

			By("Running toto apply with older version")
			_, err = testExec.Exec("toto", "apply", "~/manifests/registry/")
			Expect(err).NotTo(HaveOccurred())
		})

		It("verifies older versions are installed", func() {
			By("Checking ripgrep downgraded to 14.1.1")
			output, err := testExec.ExecBash("~/.local/bin/rg --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("ripgrep 14.1.1"))

			By("Checking fd downgraded to 10.2.0")
			output, err = testExec.ExecBash("~/.local/bin/fd --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("fd 10.2.0"))

			By("Checking jq downgraded to 1.7.1")
			output, err = testExec.ExecBash("~/.local/bin/jq --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("jq-1.7.1"))
		})

		It("upgrades back to newer version", func() {
			By("Swapping manifest back to newer version")
			_, err := testExec.ExecBash("mv ~/manifests/registry/tools.cue ~/manifests/registry/tools.cue.old")
			Expect(err).NotTo(HaveOccurred())
			_, err = testExec.ExecBash("mv ~/manifests/registry/tools.cue.new ~/manifests/registry/tools.cue")
			Expect(err).NotTo(HaveOccurred())

			By("Running toto apply with newer version")
			_, err = testExec.Exec("toto", "apply", "~/manifests/registry/")
			Expect(err).NotTo(HaveOccurred())
		})

		It("verifies newer versions are installed after upgrade", func() {
			By("Checking ripgrep upgraded to 15.1.0")
			output, err := testExec.ExecBash("~/.local/bin/rg --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("ripgrep 15.1.0"))

			By("Checking fd upgraded to 10.3.0")
			output, err = testExec.ExecBash("~/.local/bin/fd --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("fd 10.3.0"))

			By("Checking jq upgraded to 1.8.1")
			output, err = testExec.ExecBash("~/.local/bin/jq --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("jq-1.8.1"))
		})

		It("is idempotent after version changes", func() {
			By("Running toto apply again")
			_, err := testExec.Exec("toto", "apply", "~/manifests/registry/")
			Expect(err).NotTo(HaveOccurred())
		})
	})
}
