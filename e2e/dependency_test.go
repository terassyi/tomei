//go:build e2e

package e2e

import (
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func dependencyTests() {
	BeforeAll(func() {
		// Initialize toto (may already be initialized by other tests, ignore errors)
		_, _ = testExec.Exec("toto", "init", "--yes")
	})

	Context("Circular Dependency Detection", func() {
		It("detects circular dependency between installer and tool", func() {
			By("Running toto validate on circular.cue - should detect cycle")
			output, err := testExec.Exec("toto", "validate", "~/dependency-test/circular.cue")
			Expect(err).To(HaveOccurred())
			Expect(output).To(ContainSubstring("circular dependency"))
		})

		It("detects circular dependency in three-node cycle", func() {
			By("Running toto validate on circular3.cue - should detect cycle")
			output, err := testExec.Exec("toto", "validate", "~/dependency-test/circular3.cue")
			Expect(err).To(HaveOccurred())
			Expect(output).To(ContainSubstring("circular dependency"))
		})

		It("rejects installer with both runtimeRef and toolRef", func() {
			By("Running toto validate on invalid-installer.cue - should reject")
			output, err := testExec.Exec("toto", "validate", "~/dependency-test/invalid-installer.cue")
			Expect(err).To(HaveOccurred())
			Expect(output).To(ContainSubstring("runtimeRef"))
			Expect(output).To(ContainSubstring("toolRef"))
		})
	})

	Context("Parallel Tool Installation", func() {
		BeforeAll(func() {
			// Reset state.json and clean up tools/symlinks to ensure clean state
			_, _ = testExec.ExecBash(`echo '{"runtimes":{},"tools":{},"installers":{}}' > ~/.local/share/toto/state.json`)
			// Remove tools that may have been installed by previous tests with different versions
			_, _ = testExec.ExecBash(`rm -rf ~/.local/share/toto/tools/rg ~/.local/share/toto/tools/fd ~/.local/share/toto/tools/bat`)
			_, _ = testExec.ExecBash(`rm -f ~/.local/bin/rg ~/.local/bin/fd ~/.local/bin/bat`)
		})

		It("installs multiple independent tools in parallel", func() {
			By("Running toto validate on parallel.cue")
			output, err := testExec.Exec("toto", "validate", "~/dependency-test/parallel.cue")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Validation successful"))

			By("Running toto apply on parallel.cue")
			_, err = testExec.Exec("toto", "apply", "~/dependency-test/parallel.cue")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying rg works")
			output, err = testExec.ExecBash("~/.local/bin/rg --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("ripgrep " + versions.DepRgVersion))

			By("Verifying fd works")
			output, err = testExec.ExecBash("~/.local/bin/fd --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("fd " + versions.DepFdVersion))

			By("Verifying bat works")
			output, err = testExec.ExecBash("~/.local/bin/bat --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("bat " + versions.DepBatVersion))
		})

		It("is idempotent - second apply reports no changes", func() {
			By("Running toto apply again on parallel.cue")
			_, err := testExec.Exec("toto", "apply", "~/dependency-test/parallel.cue")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying tools still work after second apply")
			output, err := testExec.ExecBash("~/.local/bin/rg --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("ripgrep"))
		})
	})

	Context("Parallel Flag Behavior", func() {
		BeforeAll(func() {
			// Reset state.json and clean up tools/symlinks to ensure clean state
			_, _ = testExec.ExecBash(`echo '{"runtimes":{},"tools":{},"installers":{}}' > ~/.local/share/toto/state.json`)
			_, _ = testExec.ExecBash(`rm -rf ~/.local/share/toto/tools/rg ~/.local/share/toto/tools/fd ~/.local/share/toto/tools/bat`)
			_, _ = testExec.ExecBash(`rm -f ~/.local/bin/rg ~/.local/bin/fd ~/.local/bin/bat`)
		})

		It("installs all tools with --parallel 1 (sequential)", func() {
			By("Running toto apply with --parallel 1")
			output, err := testExec.Exec("toto", "apply", "--parallel", "1", "~/dependency-test/parallel.cue")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying rg works")
			rgOut, err := testExec.ExecBash("~/.local/bin/rg --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(rgOut).To(ContainSubstring("ripgrep " + versions.DepRgVersion))

			By("Verifying fd works")
			fdOut, err := testExec.ExecBash("~/.local/bin/fd --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(fdOut).To(ContainSubstring("fd " + versions.DepFdVersion))

			By("Verifying bat works")
			batOut, err := testExec.ExecBash("~/.local/bin/bat --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(batOut).To(ContainSubstring("bat " + versions.DepBatVersion))

			By("Verifying non-TTY output has Commands: header exactly once")
			Expect(strings.Count(output, "Commands:")).To(Equal(1))
		})

		It("shows Commands: header exactly once with default parallelism", func() {
			By("Reset state for fresh install")
			_, _ = testExec.ExecBash(`echo '{"runtimes":{},"tools":{},"installers":{}}' > ~/.local/share/toto/state.json`)
			_, _ = testExec.ExecBash(`rm -rf ~/.local/share/toto/tools/rg ~/.local/share/toto/tools/fd ~/.local/share/toto/tools/bat`)
			_, _ = testExec.ExecBash(`rm -f ~/.local/bin/rg ~/.local/bin/fd ~/.local/bin/bat`)

			By("Running toto apply without --parallel flag (default)")
			output, err := testExec.Exec("toto", "apply", "~/dependency-test/parallel.cue")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying all tools installed")
			_, err = testExec.ExecBash("~/.local/bin/rg --version")
			Expect(err).NotTo(HaveOccurred())
			_, err = testExec.ExecBash("~/.local/bin/fd --version")
			Expect(err).NotTo(HaveOccurred())
			_, err = testExec.ExecBash("~/.local/bin/bat --version")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying non-TTY output has Commands: header exactly once")
			Expect(strings.Count(output, "Commands:")).To(Equal(1))
		})
	})

	Context("Runtime and Tool Mixed Parallel Execution", func() {
		BeforeAll(func() {
			// Reset state.json to clean state
			_, _ = testExec.ExecBash(`echo '{"runtimes":{},"tools":{},"installers":{}}' > ~/.local/share/toto/state.json`)
		})

		It("installs runtime before dependent tool in parallel mode", func() {
			By("Running toto apply on runtime-chain.cue with default parallelism")
			_, err := testExec.Exec("toto", "apply", "~/dependency-test/runtime-chain.cue")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying go runtime is installed")
			output, err := testExec.ExecBash(fmt.Sprintf("GOTOOLCHAIN=local ~/.local/share/toto/runtimes/go/%s/bin/go version", versions.DepGoVersion))
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("go" + versions.DepGoVersion))

			By("Verifying gopls is installed (depends on go runtime)")
			output, err = testExec.ExecBash("~/go/bin/gopls version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("golang.org/x/tools/gopls"))

			By("Verifying state.json records both resources")
			stateOutput, err := testExec.ExecBash("cat ~/.local/share/toto/state.json")
			Expect(err).NotTo(HaveOccurred())
			Expect(stateOutput).To(ContainSubstring(`"go"`))
			Expect(stateOutput).To(ContainSubstring(`"gopls"`))
		})
	})

	Context("Tool as Installer Dependency (ToolRef)", func() {
		BeforeAll(func() {
			// Reset state.json to clean state
			_, _ = testExec.ExecBash(`echo '{"runtimes":{},"tools":{},"installers":{}}' > ~/.local/share/toto/state.json`)
		})

		It("validates toolRef dependency chain", func() {
			By("Running toto validate on toolref.cue")
			output, err := testExec.Exec("toto", "validate", "~/dependency-test/toolref.cue")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Validation successful"))

			By("Checking dependency order in output")
			Expect(output).To(ContainSubstring("Installer/aqua"))
			Expect(output).To(ContainSubstring("Tool/jq"))
			Expect(output).To(ContainSubstring("Installer/jq-installer"))
		})

		It("installs tool before dependent installer is available", func() {
			By("Running toto apply on toolref.cue")
			_, err := testExec.Exec("toto", "apply", "~/dependency-test/toolref.cue")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying jq is installed and works")
			output, err := testExec.ExecBash("~/.local/bin/jq --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("jq-" + versions.DepJqVersion))
		})
	})

	Context("Runtime to Tool Dependency Chain", func() {
		BeforeAll(func() {
			// Reset state.json to clean state
			_, _ = testExec.ExecBash(`echo '{"runtimes":{},"tools":{},"installers":{}}' > ~/.local/share/toto/state.json`)
		})

		It("validates runtime -> installer -> tool chain", func() {
			By("Running toto validate on runtime-chain.cue")
			output, err := testExec.Exec("toto", "validate", "~/dependency-test/runtime-chain.cue")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Validation successful"))

			By("Checking all resources recognized")
			Expect(output).To(ContainSubstring("Runtime/go"))
			Expect(output).To(ContainSubstring("Installer/go"))
			Expect(output).To(ContainSubstring("Tool/gopls"))
		})

		It("installs runtime before dependent tools", func() {
			By("Running toto apply on runtime-chain.cue")
			_, err := testExec.Exec("toto", "apply", "~/dependency-test/runtime-chain.cue")
			Expect(err).NotTo(HaveOccurred())

			By(fmt.Sprintf("Verifying go runtime %s is installed in expected location", versions.DepGoVersion))
			// Set GOTOOLCHAIN=local to prevent auto-upgrade to newer Go version
			output, err := testExec.ExecBash(fmt.Sprintf("GOTOOLCHAIN=local ~/.local/share/toto/runtimes/go/%s/bin/go version", versions.DepGoVersion))
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("go" + versions.DepGoVersion))

			By("Verifying gopls is installed")
			output, err = testExec.ExecBash("~/go/bin/gopls version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("golang.org/x/tools/gopls"))
		})
	})
}
