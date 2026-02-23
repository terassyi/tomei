//go:build e2e

package e2e

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func commandsPatternTests() {

	BeforeAll(func() {
		By("Resetting state for commands-pattern tests")
		_, _ = testExec.Exec("tomei", "init", "--yes", "--force")
		_, _ = testExec.ExecBash(`echo '{"runtimes":{},"tools":{},"installers":{},"installerRepositories":{}}' > ~/.local/share/tomei/state.json`)
		// Clean up any leftover artifacts from previous runs
		_, _ = testExec.ExecBash("rm -rf /tmp/tomei-cmd-update-test")
		_, _ = testExec.ExecBash("mise implode --yes 2>/dev/null")
	})

	Context("Validate and Plan", func() {
		It("validates commands-test manifests", func() {
			By("Running tomei validate on commands-test directory")
			output, err := testExec.Exec("tomei", "validate", "~/commands-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Validation successful"))
			Expect(output).To(ContainSubstring("Tool/update-tool"))
			Expect(output).To(ContainSubstring("Tool/mise"))
		})

		It("shows planned changes for commands-pattern tools", func() {
			By("Running tomei plan on commands-test directory")
			output, err := testExec.Exec("tomei", "plan", "~/commands-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Tool/update-tool"))
			Expect(output).To(ContainSubstring("Tool/mise"))
		})
	})

	Context("Install via Commands Pattern", func() {
		It("installs commands-pattern tools via apply", func() {
			By("Running tomei apply on commands-test directory")
			output, err := ExecApply(testExec, "~/commands-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("update-tool"))
			Expect(output).To(ContainSubstring("mise"))
		})

		It("creates marker file for update-tool", func() {
			By("Checking marker file exists and contains 'installed'")
			output, err := testExec.ExecBash("test -f /tmp/tomei-cmd-update-test/marker && cat /tmp/tomei-cmd-update-test/marker")
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(output)).To(Equal("installed"))
		})

		It("installs mise binary via curl | sh", func() {
			By("Checking mise binary is executable")
			output, err := testExec.ExecBash("mise --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(MatchRegexp(`\d+\.\d+\.\d+`))
		})
	})

	Context("State Recording", func() {
		It("records update-tool version from resolveVersion in state", func() {
			By("Running tomei get tools -o json")
			output, err := testExec.Exec("tomei", "get", "tools", "-o", "json")
			Expect(err).NotTo(HaveOccurred())

			By("Checking update-tool has version 2.0.0 from resolveVersion")
			Expect(output).To(ContainSubstring(`"update-tool"`))
			Expect(output).To(ContainSubstring(`"2.0.0"`))
		})

		It("records mise version from resolveVersion in state", func() {
			By("Running tomei get tools -o json")
			output, err := testExec.Exec("tomei", "get", "tools", "-o", "json")
			Expect(err).NotTo(HaveOccurred())

			By("Checking mise has a resolved semver version")
			Expect(output).To(ContainSubstring(`"mise"`))
			// resolveVersion runs: mise --version | awk '{print $1}'
			// Output is a semver like "2026.2.19"
			Expect(output).To(MatchRegexp(`"version":\s*"\d{4}\.\d+\.\d+"`))
		})

		It("shows commands-pattern tools in get output", func() {
			By("Running tomei get tools")
			output, err := testExec.Exec("tomei", "get", "tools")
			Expect(err).NotTo(HaveOccurred())

			By("Checking output contains tools with 'commands' display")
			Expect(output).To(ContainSubstring("update-tool"))
			Expect(output).To(ContainSubstring("mise"))
			Expect(output).To(ContainSubstring("commands"))
		})
	})

	Context("Idempotency", func() {
		It("is idempotent on subsequent applies", func() {
			By("Running tomei apply again")
			output, err := ExecApply(testExec, "~/commands-test/")
			Expect(err).NotTo(HaveOccurred())

			By("Checking no changes were needed")
			Expect(output).To(ContainSubstring("No changes to apply"))
		})

		It("preserves artifacts after idempotent apply", func() {
			By("Checking update-tool marker still contains 'installed'")
			output, err := testExec.ExecBash("cat /tmp/tomei-cmd-update-test/marker")
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(output)).To(Equal("installed"))

			By("Checking mise binary still works")
			output, err = testExec.ExecBash("mise --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(MatchRegexp(`\d+\.\d+\.\d+`))
		})
	})

	Context("Update with --update-tools", func() {
		It("updates commands-pattern tools with --update-tools flag", func() {
			By("Running tomei apply with --update-tools")
			output, err := ExecApply(testExec, "--update-tools", "~/commands-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("update-tool"))
		})

		It("uses update command for update-tool", func() {
			By("Checking update-tool marker was overwritten with 'updated'")
			output, err := testExec.ExecBash("cat /tmp/tomei-cmd-update-test/marker")
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(output)).To(Equal("updated"))
		})

		It("is idempotent after update", func() {
			By("Running apply without update flags")
			output, err := ExecApply(testExec, "~/commands-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("No changes to apply"))
		})
	})

	Context("Removal", func() {
		It("removes mise when manifest is hidden", func() {
			By("Hiding mise.cue manifest")
			_, err := testExec.ExecBash("mv ~/commands-test/mise.cue ~/commands-test/mise.cue.hidden")
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei apply to trigger removal of mise")
			output, err := ExecApply(testExec, "~/commands-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Removed:"))

			By("Verifying mise binary is gone")
			_, err = testExec.ExecBash("which mise")
			Expect(err).To(HaveOccurred())

			By("Verifying update-tool marker still exists")
			_, err = testExec.ExecBash("test -f /tmp/tomei-cmd-update-test/marker")
			Expect(err).NotTo(HaveOccurred())
		})

		It("restores manifests and re-installs", func() {
			By("Restoring hidden manifests")
			_, err := testExec.ExecBash("for f in ~/commands-test/*.hidden; do mv \"$f\" \"${f%.hidden}\"; done")
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei apply to re-install")
			output, err := ExecApply(testExec, "~/commands-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("mise"))

			By("Verifying mise binary works again")
			output, err = testExec.ExecBash("mise --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(MatchRegexp(`\d+\.\d+\.\d+`))
		})
	})

	AfterAll(func() {
		By("Cleaning up commands-pattern test state")
		_, _ = testExec.ExecBash("rm -rf /tmp/tomei-cmd-update-test")
		_, _ = testExec.ExecBash("mise implode --yes 2>/dev/null")
		_, _ = testExec.ExecBash("for f in ~/commands-test/*.hidden; do mv \"$f\" \"${f%.hidden}\"; done 2>/dev/null")
	})
}
