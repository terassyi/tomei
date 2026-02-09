//go:build e2e

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func logsTests() {

	BeforeAll(func() {
		// Initialize tomei (may already be initialized by other tests, ignore errors)
		_, _ = testExec.Exec("tomei", "init", "--yes")
		// Reset state to avoid interference from previous test contexts
		_, _ = testExec.ExecBash(`echo '{"runtimes":{},"tools":{},"installers":{}}' > ~/.local/share/tomei/state.json`)
		// Clean up any existing log sessions
		_, _ = testExec.ExecBash("rm -rf ~/.cache/tomei/logs")
	})

	Context("Failed Apply Log Capture", func() {
		It("captures and displays failure details on failed apply", func() {
			By("Running tomei apply on failing tool")
			output, err := testExec.Exec("tomei", "apply", "~/logs-test/")
			Expect(err).To(HaveOccurred())
			Expect(output).To(ContainSubstring("Failure Details:"))
			Expect(output).To(ContainSubstring("fail-tool"))
		})
	})

	Context("tomei logs Command", func() {
		It("lists sessions with --list flag", func() {
			output, err := testExec.Exec("tomei", "logs", "--list")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Log Sessions:"))
			Expect(output).To(MatchRegexp(`\d{8}T\d{6}`))
		})

		It("shows failed resources from latest session", func() {
			output, err := testExec.Exec("tomei", "logs")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Session:"))
			Expect(output).To(ContainSubstring("Tool/fail-tool"))
		})

		It("shows detailed log for a specific resource", func() {
			output, err := testExec.Exec("tomei", "logs", "Tool/fail-tool")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("# tomei installation log"))
			Expect(output).To(ContainSubstring("# Resource: Tool/fail-tool"))
			Expect(output).To(ContainSubstring("step 1: preparing..."))
			Expect(output).To(ContainSubstring("# Error:"))
		})

		It("returns error for nonexistent resource log", func() {
			_, err := testExec.Exec("tomei", "logs", "Tool/nonexistent")
			Expect(err).To(HaveOccurred())
		})
	})
}
