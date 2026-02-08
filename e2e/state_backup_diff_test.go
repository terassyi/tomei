//go:build e2e

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func stateBackupDiffTests() {

	BeforeAll(func() {
		By("Restoring runtime manifest layout after Basic tests")
		// After Basic's "Runtime Upgrade", runtime.cue contains the upgrade version
		// and runtime.cue.old has the original. Swap them back and recreate .upgrade.
		_, _ = testExec.ExecBash("if [ -f ~/manifests/runtime.cue.old ]; then mv ~/manifests/runtime.cue ~/manifests/runtime.cue.upgrade && mv ~/manifests/runtime.cue.old ~/manifests/runtime.cue; fi")

		By("Initializing clean environment for state backup/diff tests")
		_, err := testExec.Exec("toto", "init", "--yes", "--force")
		Expect(err).NotTo(HaveOccurred())

		By("Removing any leftover backup file")
		_, _ = testExec.ExecBash("rm -f ~/.local/share/toto/state.json.bak")

		By("Removing any leftover tools/runtimes from previous tests")
		_, _ = testExec.ExecBash("rm -rf ~/.local/share/toto/tools ~/.local/share/toto/runtimes")
		_, _ = testExec.ExecBash("rm -f ~/.local/bin/* ~/go/bin/*")
	})

	Context("Diff Before First Apply", func() {
		It("shows no backup message when backup does not exist", func() {
			By("Ensuring no backup file exists")
			_, err := testExec.ExecBash("test ! -f ~/.local/share/toto/state.json.bak")
			Expect(err).NotTo(HaveOccurred())

			By("Running toto state diff")
			output, err := testExec.Exec("toto", "state", "diff")
			Expect(err).NotTo(HaveOccurred())

			By("Checking output contains 'No backup found' message")
			Expect(output).To(ContainSubstring("No backup found"))
		})
	})

	Context("Backup Creation", func() {
		It("creates state.json.bak during apply", func() {
			By("Recording state.json content before apply")
			stateBefore, err := testExec.ExecBash("cat ~/.local/share/toto/state.json")
			Expect(err).NotTo(HaveOccurred())

			By("Running toto apply to install runtime and tools")
			_, err = testExec.Exec("toto", "apply", "~/manifests/")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying state.json.bak was created")
			_, err = testExec.ExecBash("test -f ~/.local/share/toto/state.json.bak")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying backup content matches pre-apply state")
			backupContent, err := testExec.ExecBash("cat ~/.local/share/toto/state.json.bak")
			Expect(err).NotTo(HaveOccurred())
			Expect(backupContent).To(Equal(stateBefore))
		})

		It("backup differs from current state after first apply", func() {
			By("Reading current state.json")
			stateCurrent, err := testExec.ExecBash("cat ~/.local/share/toto/state.json")
			Expect(err).NotTo(HaveOccurred())

			By("Checking current state has installed resources")
			Expect(stateCurrent).To(ContainSubstring(fmt.Sprintf(`"version": "%s"`, versions.GoVersion)))
			Expect(stateCurrent).To(ContainSubstring(fmt.Sprintf(`"version": "%s"`, versions.GhVersion)))

			By("Reading backup state")
			backupContent, err := testExec.ExecBash("cat ~/.local/share/toto/state.json.bak")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying backup does NOT contain installed tool versions")
			Expect(backupContent).NotTo(ContainSubstring(fmt.Sprintf(`"version": "%s"`, versions.GhVersion)))
		})
	})

	Context("Diff After First Apply", func() {
		It("shows added resources in text format", func() {
			By("Running toto state diff")
			output, err := testExec.Exec("toto", "state", "diff")
			Expect(err).NotTo(HaveOccurred())

			By("Checking header is shown")
			Expect(output).To(ContainSubstring("State changes"))

			By("Checking go runtime is shown as added")
			Expect(output).To(ContainSubstring("go"))
			Expect(output).To(ContainSubstring(versions.GoVersion))

			By("Checking gh tool is shown as added")
			Expect(output).To(ContainSubstring("gh"))
			Expect(output).To(ContainSubstring(versions.GhVersion))

			By("Checking gopls tool is shown as added")
			Expect(output).To(ContainSubstring("gopls"))

			By("Checking summary line")
			Expect(output).To(ContainSubstring("added"))
			Expect(output).To(ContainSubstring("Summary"))
		})

		It("shows added resources in JSON format", func() {
			By("Running toto state diff --output json")
			output, err := testExec.Exec("toto", "state", "diff", "--output", "json")
			Expect(err).NotTo(HaveOccurred())

			By("Checking JSON contains changes array")
			Expect(output).To(ContainSubstring(`"changes"`))

			By("Checking JSON contains added type")
			Expect(output).To(ContainSubstring(`"type": "added"`))

			By("Checking JSON contains runtime entry")
			Expect(output).To(ContainSubstring(`"kind": "runtime"`))
			Expect(output).To(ContainSubstring(`"name": "go"`))

			By("Checking JSON contains tool entry")
			Expect(output).To(ContainSubstring(`"kind": "tool"`))
			Expect(output).To(ContainSubstring(`"name": "gh"`))
		})

		It("supports --no-color flag", func() {
			By("Running toto state diff --no-color")
			output, err := testExec.Exec("toto", "state", "diff", "--no-color")
			Expect(err).NotTo(HaveOccurred())

			By("Checking output does not contain ANSI escape codes")
			Expect(output).NotTo(MatchRegexp(`\x1b\[`))

			By("Checking output still contains diff content")
			Expect(output).To(ContainSubstring("State changes"))
			Expect(output).To(ContainSubstring("go"))
		})
	})

	Context("Diff After Idempotent Apply", func() {
		It("shows no changes after idempotent apply", func() {
			By("Running toto apply again (idempotent)")
			_, err := testExec.Exec("toto", "apply", "~/manifests/")
			Expect(err).NotTo(HaveOccurred())

			By("Running toto state diff")
			output, err := testExec.Exec("toto", "state", "diff")
			Expect(err).NotTo(HaveOccurred())

			By("Checking output says no changes")
			Expect(output).To(ContainSubstring("No changes since last apply"))
		})
	})

	Context("Diff After Version Upgrade", func() {
		It("creates backup with old version after upgrade", func() {
			By(fmt.Sprintf("Swapping runtime config to upgraded version (%s -> %s)", versions.GoVersion, versions.GoVersionUpgrade))
			_, err := testExec.ExecBash("mv ~/manifests/runtime.cue ~/manifests/runtime.cue.old")
			Expect(err).NotTo(HaveOccurred())
			_, err = testExec.ExecBash("mv ~/manifests/runtime.cue.upgrade ~/manifests/runtime.cue")
			Expect(err).NotTo(HaveOccurred())

			By("Running toto apply with upgraded config")
			_, err = testExec.Exec("toto", "apply", "~/manifests/")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying backup contains old version")
			backupContent, err := testExec.ExecBash("cat ~/.local/share/toto/state.json.bak")
			Expect(err).NotTo(HaveOccurred())
			Expect(backupContent).To(ContainSubstring(fmt.Sprintf(`"version": "%s"`, versions.GoVersion)))

			By("Verifying current state contains new version")
			stateContent, err := testExec.ExecBash("cat ~/.local/share/toto/state.json")
			Expect(err).NotTo(HaveOccurred())
			Expect(stateContent).To(ContainSubstring(fmt.Sprintf(`"version": "%s"`, versions.GoVersionUpgrade)))
		})

		It("shows runtime modification in text diff", func() {
			By("Running toto state diff")
			output, err := testExec.Exec("toto", "state", "diff")
			Expect(err).NotTo(HaveOccurred())

			By("Checking diff shows runtime version change")
			Expect(output).To(ContainSubstring("State changes"))
			Expect(output).To(ContainSubstring("go"))
			Expect(output).To(ContainSubstring(versions.GoVersion))
			Expect(output).To(ContainSubstring(versions.GoVersionUpgrade))

			By("Checking summary includes modified count")
			Expect(output).To(ContainSubstring("modified"))
		})

		It("shows runtime modification in JSON diff", func() {
			By("Running toto state diff --output json")
			output, err := testExec.Exec("toto", "state", "diff", "--output", "json")
			Expect(err).NotTo(HaveOccurred())

			By("Checking JSON contains modified type for runtime")
			Expect(output).To(ContainSubstring(`"type": "modified"`))
			Expect(output).To(ContainSubstring(`"name": "go"`))
			Expect(output).To(ContainSubstring(fmt.Sprintf(`"oldVersion": "%s"`, versions.GoVersion)))
			Expect(output).To(ContainSubstring(fmt.Sprintf(`"newVersion": "%s"`, versions.GoVersionUpgrade)))
		})

		AfterAll(func() {
			By("Restoring original runtime manifest")
			_, _ = testExec.ExecBash("mv ~/manifests/runtime.cue ~/manifests/runtime.cue.upgrade")
			_, _ = testExec.ExecBash("mv ~/manifests/runtime.cue.old ~/manifests/runtime.cue")

			By("Restoring original runtime version via apply")
			_, _ = testExec.Exec("toto", "apply", "~/manifests/")
		})
	})

	Context("Diff After Resource Removal", func() {
		It("shows removed tool in text diff", func() {
			By("Hiding tool manifest to trigger removal")
			_, err := testExec.ExecBash("mv ~/manifests/tools.cue ~/manifests/tools.cue.hidden")
			Expect(err).NotTo(HaveOccurred())

			By("Running toto apply")
			_, err = testExec.Exec("toto", "apply", "~/manifests/")
			Expect(err).NotTo(HaveOccurred())

			By("Running toto state diff")
			output, err := testExec.Exec("toto", "state", "diff")
			Expect(err).NotTo(HaveOccurred())

			By("Checking diff shows gh as removed")
			Expect(output).To(ContainSubstring("gh"))
			Expect(output).To(ContainSubstring("removed"))
		})

		It("shows removed tool in JSON diff", func() {
			By("Running toto state diff --output json")
			output, err := testExec.Exec("toto", "state", "diff", "--output", "json")
			Expect(err).NotTo(HaveOccurred())

			By("Checking JSON contains removed type for gh")
			Expect(output).To(ContainSubstring(`"type": "removed"`))
			Expect(output).To(ContainSubstring(`"name": "gh"`))
		})

		AfterAll(func() {
			By("Restoring tool manifest")
			_, _ = testExec.ExecBash("mv ~/manifests/tools.cue.hidden ~/manifests/tools.cue")
		})
	})

	Context("Backup Overwrite", func() {
		It("overwrites backup on each apply", func() {
			By("Recording current backup content")
			backupBefore, err := testExec.ExecBash("cat ~/.local/share/toto/state.json.bak")
			Expect(err).NotTo(HaveOccurred())

			By("Running toto apply again")
			_, err = testExec.Exec("toto", "apply", "~/manifests/")
			Expect(err).NotTo(HaveOccurred())

			By("Reading new backup content")
			backupAfter, err := testExec.ExecBash("cat ~/.local/share/toto/state.json.bak")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying backup was updated")
			Expect(backupAfter).NotTo(Equal(backupBefore))
		})
	})
}
