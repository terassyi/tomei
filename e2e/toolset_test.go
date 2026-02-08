//go:build e2e

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func toolsetTests() {

	BeforeAll(func() {
		By("Initializing environment")
		_, err := testExec.Exec("tomei", "init", "--yes", "--force")
		Expect(err).NotTo(HaveOccurred())

		By("Cleaning previous state")
		_, _ = testExec.ExecBash("rm -rf ~/.local/share/tomei/tools ~/.local/bin/* ~/go/bin/*")
	})

	Context("Validation", func() {
		It("validates ToolSet manifest with runtime", func() {
			By("Running tomei validate on toolset + runtime")
			output, err := testExec.Exec("tomei", "validate", "~/manifests/toolset.cue", "~/manifests/runtime.cue")
			Expect(err).NotTo(HaveOccurred())

			By("Checking validation succeeded")
			Expect(output).To(ContainSubstring("Validation successful"))

			By("Checking expanded tools are listed")
			Expect(output).To(ContainSubstring("Tool/staticcheck"))
			Expect(output).To(ContainSubstring("Tool/godoc"))
		})
	})

	Context("Installation", func() {
		It("installs all tools from ToolSet via runtime delegation", func() {
			By("Running tomei apply on toolset + runtime")
			output, err := testExec.Exec("tomei", "apply", "~/manifests/toolset.cue", "~/manifests/runtime.cue")
			Expect(err).NotTo(HaveOccurred())

			By("Checking apply completed")
			Expect(output).To(ContainSubstring("Apply complete"))
		})

		It("installed staticcheck binary", func() {
			By("Running staticcheck -version")
			output, err := testExec.ExecBash("~/go/bin/staticcheck -version 2>&1")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("staticcheck"))
		})

		It("installed godoc binary", func() {
			By("Running godoc -help")
			output, err := testExec.ExecBash("~/go/bin/godoc -h 2>&1 || true")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("usage"))
		})

		It("saved individual tool states", func() {
			By("Checking state.json contains staticcheck and godoc")
			output, err := testExec.ExecBash("cat ~/.local/share/tomei/state.json")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring(`"staticcheck"`))
			Expect(output).To(ContainSubstring(`"godoc"`))
		})
	})

	Context("Re-apply (idempotent)", func() {
		It("reports no changes on re-apply", func() {
			By("Running tomei apply again")
			output, err := testExec.Exec("tomei", "apply", "~/manifests/toolset.cue", "~/manifests/runtime.cue")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("No changes to apply"))
		})
	})
}
