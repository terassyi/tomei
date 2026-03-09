//go:build e2e

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func binaryNameTests() {

	BeforeAll(func() {
		By("Running tomei init to ensure state.json exists")
		_, err := testExec.Exec("tomei", "init", "--yes", "--force")
		Expect(err).NotTo(HaveOccurred())
	})

	Context("BinaryName Override", func() {
		It("validates binary-name-test manifest", func() {
			By("Running tomei validate on binary-name-test manifest")
			output, err := testExec.Exec("tomei", "validate", "~/binary-name-test/")
			Expect(err).NotTo(HaveOccurred())

			By("Checking validation succeeded")
			Expect(output).To(ContainSubstring("Validation successful"))

			By("Checking Tool/krew is recognized")
			Expect(output).To(ContainSubstring("Tool/krew"))
		})

		It("installs tool with binaryName override", func() {
			By("Running tomei apply on binary-name-test manifest")
			output, err := ExecApply(testExec, "~/binary-name-test/")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying apply completed")
			Expect(output).To(ContainSubstring("Apply complete!"))
		})

		It("creates symlink with overridden name", func() {
			By("Checking kubectl-krew symlink exists")
			_, err := testExec.ExecBash("ls ~/.local/bin/kubectl-krew")
			Expect(err).NotTo(HaveOccurred())

			By("Checking krew symlink does NOT exist (binaryName overrides)")
			_, err = testExec.ExecBash("test ! -e ~/.local/bin/krew")
			Expect(err).NotTo(HaveOccurred())
		})

		It("binary is executable", func() {
			By("Executing kubectl-krew version")
			output, err := testExec.ExecBash("~/.local/bin/kubectl-krew version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring(versions.BinaryNameKrewVersion))
		})

		It("state records correct BinPath", func() {
			By("Reading state.json")
			output, err := testExec.ExecBash("cat ~/.local/share/tomei/state.json")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("kubectl-krew"))
		})

		It("idempotent apply has no changes", func() {
			By("Running tomei apply again")
			output, err := ExecApply(testExec, "~/binary-name-test/")
			Expect(err).NotTo(HaveOccurred())
			fmt.Fprintf(GinkgoWriter, "Idempotent apply output: %s\n", output)
			Expect(output).To(ContainSubstring("No changes to apply"))
		})
	})
}
