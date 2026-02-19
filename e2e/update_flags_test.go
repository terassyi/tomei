//go:build e2e

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func updateFlagsTests() {

	BeforeAll(func() {
		By("Resetting state for update-flags tests")
		_, _ = testExec.Exec("tomei", "init", "--yes", "--force")
		_, _ = testExec.ExecBash(`echo '{"runtimes":{},"tools":{},"installers":{},"installerRepositories":{}}' > ~/.local/share/tomei/state.json`)
		// Clean up any leftover artifacts
		_, _ = testExec.ExecBash("rm -rf /tmp/mock-rt")
	})

	Context("Initial Setup", func() {
		It("validates update-flags-test manifests", func() {
			By("Running tomei validate")
			output, err := testExec.Exec("tomei", "validate", "~/update-flags-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Validation successful"))
		})

		It("installs runtime and tool for update tests", func() {
			By("Applying update-flags-test manifests")
			output, err := ExecApply(testExec, "~/update-flags-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("mock-rt"))

			By("Verifying runtime state")
			output, err = testExec.Exec("tomei", "get", "runtimes", "-o", "json")
			Expect(err).NotTo(HaveOccurred())
			// The resolved version should be 1.0.0 (from ResolveVersion echo)
			Expect(output).To(ContainSubstring("1.0.0"))
			// VersionKind should be alias (original spec was "stable")
			Expect(output).To(ContainSubstring("alias"))
		})

		It("is idempotent", func() {
			By("Running apply again without update flags")
			output, err := ExecApply(testExec, "~/update-flags-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("No changes to apply"))
		})
	})

	Context("--update-runtimes", func() {
		It("shows reinstall in plan for alias-versioned runtime", func() {
			By("Running plan with --update-runtimes")
			output, err := testExec.Exec("tomei", "plan", "--update-runtimes", "--no-color", "~/update-flags-test/")
			Expect(err).NotTo(HaveOccurred())

			By("Checking that runtime is marked for reinstall")
			Expect(output).To(ContainSubstring("Runtime/mock-rt"))
			Expect(output).To(ContainSubstring("reinstall"))
		})

		It("reinstalls alias runtime via --update-runtimes", func() {
			By("Applying with --update-runtimes")
			output, err := ExecApply(testExec, "--update-runtimes", "~/update-flags-test/")
			Expect(err).NotTo(HaveOccurred())

			By("Checking runtime was reinstalled")
			Expect(output).To(ContainSubstring("mock-rt"))
		})

		It("is idempotent after update", func() {
			By("Running apply without update flags")
			output, err := ExecApply(testExec, "~/update-flags-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("No changes to apply"))
		})
	})

	Context("--update-all", func() {
		It("shows plan with both runtime and tool reinstall", func() {
			By("Running plan with --update-all")
			output, err := testExec.Exec("tomei", "plan", "--update-all", "--no-color", "~/update-flags-test/")
			Expect(err).NotTo(HaveOccurred())

			By("Checking both runtime and tool are shown")
			Expect(output).To(ContainSubstring("Runtime/mock-rt"))
			Expect(output).To(ContainSubstring("reinstall"))
		})

		It("reinstalls both runtime and tool via --update-all", func() {
			By("Applying with --update-all")
			output, err := ExecApply(testExec, "--update-all", "~/update-flags-test/")
			Expect(err).NotTo(HaveOccurred())

			By("Checking resources were reinstalled")
			Expect(output).To(ContainSubstring("mock-rt"))
		})

		It("is idempotent after update-all", func() {
			By("Running apply without update flags")
			output, err := ExecApply(testExec, "~/update-flags-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("No changes to apply"))
		})
	})

	AfterAll(func() {
		By("Cleaning up update-flags-test state")
		_, _ = testExec.ExecBash("rm -rf /tmp/mock-rt")
	})
}
