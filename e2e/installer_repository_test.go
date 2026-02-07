//go:build e2e

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Installer Repository tests use helm (latest via aqua) to test delegation-based
// InstallerRepository management and the InstallerRepository → Tool dependency chain.

func installerRepositoryTests() {

	BeforeAll(func() {
		// Clean up any pre-existing helm repos and binaries (before init resets state)
		_, _ = testExec.ExecBash("helm repo remove bitnami 2>/dev/null || true")
		_, _ = testExec.ExecBash("rm -f ~/.local/bin/helm")
		_, _ = testExec.ExecBash("rm -rf ~/.local/share/toto/tools/helm")
		// Initialize toto with --force: creates clean state.json with registry info (required for aqua resolver)
		_, _ = testExec.Exec("toto", "init", "--yes", "--force")
	})

	Context("Delegation: Helm Repository", func() {
		It("validates helm-repo manifest", func() {
			By("Running toto validate on helm-repo.cue")
			output, err := testExec.Exec("toto", "validate", "~/installer-repo-test/helm-repo.cue")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Validation successful"))

			By("Checking Tool/helm is recognized")
			Expect(output).To(ContainSubstring("Tool/helm"))

			By("Checking InstallerRepository/bitnami is recognized")
			Expect(output).To(ContainSubstring("InstallerRepository/bitnami"))
		})

		It("installs helm and adds bitnami repository", func() {
			By("Running toto apply on helm-repo.cue")
			_, err := testExec.Exec("toto", "apply", "~/installer-repo-test/helm-repo.cue")
			Expect(err).NotTo(HaveOccurred())
		})

		It("helm binary is available", func() {
			By("Executing helm version")
			output, err := testExec.ExecBash("~/.local/bin/helm version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Version:"))
		})

		It("bitnami repository is registered in helm", func() {
			By("Checking helm repo list")
			output, err := testExec.ExecBash("~/.local/bin/helm repo list")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("bitnami"))
		})

		It("records InstallerRepository in state.json", func() {
			By("Reading state.json")
			output, err := testExec.ExecBash("cat ~/.local/share/toto/state.json")
			Expect(err).NotTo(HaveOccurred())

			By("Checking bitnami is in installerRepositories section")
			Expect(output).To(ContainSubstring(`"bitnami"`))

			By("Checking sourceType is delegation")
			Expect(output).To(ContainSubstring(`"sourceType": "delegation"`))

			By("Checking installerRef is helm")
			Expect(output).To(ContainSubstring(`"installerRef": "helm"`))
		})

		It("is idempotent on subsequent apply", func() {
			By("Running toto apply again")
			_, err := testExec.Exec("toto", "apply", "~/installer-repo-test/helm-repo.cue")
			Expect(err).NotTo(HaveOccurred())

			By("Checking bitnami still registered")
			output, err := testExec.ExecBash("~/.local/bin/helm repo list")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("bitnami"))
		})
	})

	Context("Dependency Chain: InstallerRepository → Tool (helm pull)", func() {
		BeforeAll(func() {
			// Clean up helm repos and binaries
			_, _ = testExec.ExecBash("helm repo remove bitnami 2>/dev/null || true")
			_, _ = testExec.ExecBash("rm -f ~/.local/bin/helm")
			_, _ = testExec.ExecBash("rm -rf ~/.local/share/toto/tools/helm")
			// Clean up chart download destination
			_, _ = testExec.ExecBash("rm -rf /tmp/toto-e2e-charts")
			// Re-initialize toto with --force: creates clean state.json with registry info
			_, _ = testExec.Exec("toto", "init", "--yes", "--force")
		})

		It("validates repo-with-tool manifest", func() {
			By("Running toto validate on repo-with-tool.cue")
			output, err := testExec.Exec("toto", "validate", "~/installer-repo-test/repo-with-tool.cue")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Validation successful"))

			By("Checking Tool/helm is recognized")
			Expect(output).To(ContainSubstring("Tool/helm"))

			By("Checking InstallerRepository/bitnami is recognized")
			Expect(output).To(ContainSubstring("InstallerRepository/bitnami"))

			By("Checking Tool/common-chart is recognized")
			Expect(output).To(ContainSubstring("Tool/common-chart"))
		})

		It("installs InstallerRepository then pulls chart via dependent Tool", func() {
			By("Running toto apply on repo-with-tool.cue")
			_, err := testExec.Exec("toto", "apply", "~/installer-repo-test/repo-with-tool.cue")
			Expect(err).NotTo(HaveOccurred())
		})

		It("bitnami repository is registered", func() {
			By("Checking helm repo list")
			output, err := testExec.ExecBash("~/.local/bin/helm repo list")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("bitnami"))
		})

		It("chart tgz was downloaded", func() {
			By("Checking /tmp/toto-e2e-charts/common-*.tgz exists")
			_, err := testExec.ExecBash("ls /tmp/toto-e2e-charts/common-*.tgz")
			Expect(err).NotTo(HaveOccurred())
		})

		It("state.json records common-chart tool and bitnami repository", func() {
			By("Reading state.json")
			output, err := testExec.ExecBash("cat ~/.local/share/toto/state.json")
			Expect(err).NotTo(HaveOccurred())

			By("Checking common-chart is in tools")
			Expect(output).To(ContainSubstring(`"common-chart"`))

			By("Checking bitnami is in installerRepositories")
			Expect(output).To(ContainSubstring(`"bitnami"`))
		})

		It("is idempotent on subsequent apply", func() {
			By("Running toto apply again")
			_, err := testExec.Exec("toto", "apply", "~/installer-repo-test/repo-with-tool.cue")
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("Removal", func() {
		It("removes InstallerRepository and Tool when manifest reduced", func() {
			By("Applying helm-only manifest (no InstallerRepository, no common-chart)")
			_, err := testExec.Exec("toto", "apply", "~/installer-repo-test/helm-only.cue")
			Expect(err).NotTo(HaveOccurred())

			By("Checking bitnami is NOT in helm repo list")
			output, err := testExec.ExecBash("~/.local/bin/helm repo list 2>&1 || true")
			Expect(output).NotTo(ContainSubstring("bitnami"))

			By("Checking state.json does not contain bitnami")
			output, err = testExec.ExecBash("cat ~/.local/share/toto/state.json")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).NotTo(ContainSubstring(`"bitnami"`))

			By("Checking state.json does not contain common-chart")
			Expect(output).NotTo(ContainSubstring(`"common-chart"`))
		})
	})
}
