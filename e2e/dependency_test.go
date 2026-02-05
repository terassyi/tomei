package e2e_test

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Dependency Resolution", Ordered, func() {
	BeforeAll(func() {
		containerName = os.Getenv("TOTO_E2E_CONTAINER")
		if containerName == "" {
			Skip("TOTO_E2E_CONTAINER environment variable is not set - skipping E2E tests")
		}

		// Initialize toto (may already be initialized by other tests, ignore errors)
		_, _ = containerExec("toto", "init", "--yes")
	})

	Describe("Circular Dependency Detection", func() {
		It("detects circular dependency between installer and tool", func() {
			By("Running toto validate on circular.cue - should detect cycle")
			output, err := containerExec("toto", "validate", "~/dependency-test/circular.cue")
			Expect(err).To(HaveOccurred())
			Expect(output).To(ContainSubstring("circular dependency"))
		})

		It("detects circular dependency in three-node cycle", func() {
			By("Running toto validate on circular3.cue - should detect cycle")
			output, err := containerExec("toto", "validate", "~/dependency-test/circular3.cue")
			Expect(err).To(HaveOccurred())
			Expect(output).To(ContainSubstring("circular dependency"))
		})

		It("rejects installer with both runtimeRef and toolRef", func() {
			By("Running toto validate on invalid-installer.cue - should reject")
			output, err := containerExec("toto", "validate", "~/dependency-test/invalid-installer.cue")
			Expect(err).To(HaveOccurred())
			Expect(output).To(ContainSubstring("runtimeRef"))
			Expect(output).To(ContainSubstring("toolRef"))
		})
	})

	Describe("Parallel Tool Installation", func() {
		BeforeAll(func() {
			// Reset state.json and clean up tools/symlinks to ensure clean state
			_, _ = containerExecBash(`echo '{"runtimes":{},"tools":{},"installers":{}}' > ~/.local/share/toto/state.json`)
			// Remove tools that may have been installed by previous tests with different versions
			_, _ = containerExecBash(`rm -rf ~/.local/share/toto/tools/rg ~/.local/share/toto/tools/fd ~/.local/share/toto/tools/bat`)
			_, _ = containerExecBash(`rm -f ~/.local/bin/rg ~/.local/bin/fd ~/.local/bin/bat`)
		})

		It("installs multiple independent tools in parallel", func() {
			By("Running toto validate on parallel.cue")
			output, err := containerExec("toto", "validate", "~/dependency-test/parallel.cue")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Validation successful"))

			By("Running toto apply on parallel.cue")
			output, err = containerExec("toto", "apply", "~/dependency-test/parallel.cue")
			Expect(err).NotTo(HaveOccurred())

			By("Checking all tools were installed")
			Expect(output).To(ContainSubstring("name=rg"))
			Expect(output).To(ContainSubstring("name=fd"))
			Expect(output).To(ContainSubstring("name=bat"))

			By("Verifying rg works")
			output, err = containerExecBash("~/.local/bin/rg --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("ripgrep 14.1.1"))

			By("Verifying fd works")
			output, err = containerExecBash("~/.local/bin/fd --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("fd 10.2.0"))

			By("Verifying bat works")
			output, err = containerExecBash("~/.local/bin/bat --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("bat 0.24.0"))
		})

		It("is idempotent - second apply reports no changes", func() {
			By("Running toto apply again on parallel.cue")
			output, err := containerExec("toto", "apply", "~/dependency-test/parallel.cue")
			Expect(err).NotTo(HaveOccurred())

			By("Checking no changes needed")
			Expect(output).To(SatisfyAny(
				ContainSubstring("no changes"),
				ContainSubstring("total_actions=0"),
			))
		})
	})

	Describe("Tool as Installer Dependency (ToolRef)", func() {
		BeforeAll(func() {
			// Reset state.json to clean state
			_, _ = containerExecBash(`echo '{"runtimes":{},"tools":{},"installers":{}}' > ~/.local/share/toto/state.json`)
		})

		It("validates toolRef dependency chain", func() {
			By("Running toto validate on toolref.cue")
			output, err := containerExec("toto", "validate", "~/dependency-test/toolref.cue")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Validation successful"))

			By("Checking dependency order in output")
			Expect(output).To(ContainSubstring("Installer/aqua"))
			Expect(output).To(ContainSubstring("Tool/jq"))
			Expect(output).To(ContainSubstring("Installer/jq-installer"))
		})

		It("installs tool before dependent installer is available", func() {
			By("Running toto apply on toolref.cue")
			output, err := containerExec("toto", "apply", "~/dependency-test/toolref.cue")
			Expect(err).NotTo(HaveOccurred())

			By("Checking jq tool was installed")
			Expect(output).To(ContainSubstring("name=jq"))

			By("Verifying jq is installed and works")
			output, err = containerExecBash("~/.local/bin/jq --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("jq-1.7"))
		})
	})

	Describe("Runtime to Tool Dependency Chain", func() {
		BeforeAll(func() {
			// Reset state.json to clean state
			_, _ = containerExecBash(`echo '{"runtimes":{},"tools":{},"installers":{}}' > ~/.local/share/toto/state.json`)
		})

		It("validates runtime -> installer -> tool chain", func() {
			By("Running toto validate on runtime-chain.cue")
			output, err := containerExec("toto", "validate", "~/dependency-test/runtime-chain.cue")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Validation successful"))

			By("Checking all resources recognized")
			Expect(output).To(ContainSubstring("Runtime/go"))
			Expect(output).To(ContainSubstring("Installer/go"))
			Expect(output).To(ContainSubstring("Tool/gopls"))
		})

		It("installs runtime before dependent tools", func() {
			By("Running toto apply on runtime-chain.cue")
			output, err := containerExec("toto", "apply", "~/dependency-test/runtime-chain.cue")
			Expect(err).NotTo(HaveOccurred())

			By("Checking runtime was installed")
			Expect(output).To(ContainSubstring("installing runtime"))
			Expect(output).To(ContainSubstring("name=go"))

			By("Checking tool was installed after runtime")
			Expect(output).To(ContainSubstring("installing tool"))
			Expect(output).To(ContainSubstring("name=gopls"))

			By("Verifying go runtime 1.23.5 is installed in expected location")
			// Set GOTOOLCHAIN=local to prevent auto-upgrade to newer Go version
			output, err = containerExecBash("GOTOOLCHAIN=local ~/.local/share/toto/runtimes/go/1.23.5/bin/go version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("go1.23"))

			By("Verifying gopls is installed")
			output, err = containerExecBash("~/go/bin/gopls version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("golang.org/x/tools/gopls"))
		})
	})
})
