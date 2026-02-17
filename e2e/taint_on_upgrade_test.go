//go:build e2e

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func taintOnUpgradeTests() {

	BeforeAll(func() {
		By("Resetting state for taintOnUpgrade tests")
		_, _ = testExec.Exec("tomei", "init", "--yes", "--force")
		_, _ = testExec.ExecBash(`echo '{"runtimes":{},"tools":{},"installers":{},"installerRepositories":{}}' > ~/.local/share/tomei/state.json`)
		// Remove any leftover runtime/tool artifacts
		_, _ = testExec.ExecBash("rm -rf ~/.local/share/tomei/runtimes/go ~/.local/share/tomei/tools/gopls")
		_, _ = testExec.ExecBash("rm -f ~/go/bin/go ~/go/bin/gofmt ~/go/bin/gopls")
	})

	Context("Setup", func() {
		It("installs runtime and delegation tool (taintOnUpgrade=false)", func() {
			By("Applying taint-on-upgrade-test manifests with default taintOnUpgrade (false)")
			_, err := ExecApply(testExec, "~/taint-on-upgrade-test/")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying go runtime is installed")
			output, err := testExec.ExecBash("GOTOOLCHAIN=local ~/go/bin/go version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("go" + versions.GoVersion))

			By("Verifying gopls is installed via delegation")
			output, err = testExec.ExecBash("~/go/bin/gopls version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("golang.org/x/tools/gopls"))
		})
	})

	Context("Upgrade without taintOnUpgrade", func() {
		It("does not predict taint reinstall in plan", func() {
			By("Swapping runtime to upgraded version (still without taintOnUpgrade)")
			_, err := testExec.ExecBash("mv ~/taint-on-upgrade-test/runtime.cue ~/taint-on-upgrade-test/runtime.cue.old")
			Expect(err).NotTo(HaveOccurred())
			_, err = testExec.ExecBash("mv ~/taint-on-upgrade-test/runtime.cue.upgrade ~/taint-on-upgrade-test/runtime.cue")
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei plan")
			output, err := testExec.Exec("tomei", "plan", "--no-color", "~/taint-on-upgrade-test/")
			Expect(err).NotTo(HaveOccurred())

			By("Checking plan shows runtime upgrade")
			Expect(output).To(ContainSubstring("Runtime/go"))

			By("Checking plan does NOT predict reinstall for dependent tools")
			Expect(output).To(ContainSubstring("0 to reinstall"))
			Expect(output).NotTo(ContainSubstring("reinstall]"))
		})

		It("upgrades runtime without tainting dependent tools", func() {
			By("Applying upgraded config")
			output, err := ExecApply(testExec, "~/taint-on-upgrade-test/")
			Expect(err).NotTo(HaveOccurred())

			By("Checking apply summary does NOT show reinstalled tools")
			Expect(output).NotTo(ContainSubstring("Reinstalled:"))

			By("Verifying new runtime version is installed")
			goOutput, err := testExec.ExecBash("GOTOOLCHAIN=local ~/go/bin/go version")
			Expect(err).NotTo(HaveOccurred())
			Expect(goOutput).To(ContainSubstring("go" + versions.GoVersionUpgrade))

			By("Verifying gopls still works (was not reinstalled)")
			goplsOutput, err := testExec.ExecBash("~/go/bin/gopls version")
			Expect(err).NotTo(HaveOccurred())
			Expect(goplsOutput).To(ContainSubstring("golang.org/x/tools/gopls"))
		})

		It("shows no taint reason in get tools output", func() {
			By("Running tomei get tools")
			output, err := testExec.Exec("tomei", "get", "tools")
			Expect(err).NotTo(HaveOccurred())

			By("Checking TAINTED column header exists")
			Expect(output).To(ContainSubstring("TAINTED"))

			By("Checking gopls has no taint reason")
			Expect(output).NotTo(ContainSubstring("runtime_upgraded"))
		})
	})

	Context("Upgrade with taintOnUpgrade enabled", func() {
		BeforeAll(func() {
			By("Restoring original runtime version to prepare for re-upgrade")
			_, err := testExec.ExecBash("mv ~/taint-on-upgrade-test/runtime.cue ~/taint-on-upgrade-test/runtime.cue.upgrade")
			Expect(err).NotTo(HaveOccurred())
			_, err = testExec.ExecBash("mv ~/taint-on-upgrade-test/runtime.cue.old ~/taint-on-upgrade-test/runtime.cue")
			Expect(err).NotTo(HaveOccurred())

			By("Downgrading back to original version")
			_, err = ExecApply(testExec, "~/taint-on-upgrade-test/")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying runtime is back to original version")
			output, err := testExec.ExecBash("GOTOOLCHAIN=local ~/go/bin/go version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("go" + versions.GoVersion))
		})

		It("predicts taint reinstall in plan when taintOnUpgrade is true", func() {
			By("Swapping runtime to taint-enabled upgrade version")
			_, err := testExec.ExecBash("mv ~/taint-on-upgrade-test/runtime.cue ~/taint-on-upgrade-test/runtime.cue.old")
			Expect(err).NotTo(HaveOccurred())
			_, err = testExec.ExecBash("mv ~/taint-on-upgrade-test/runtime-taint-enabled.cue.upgrade ~/taint-on-upgrade-test/runtime.cue")
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei plan")
			output, err := testExec.Exec("tomei", "plan", "--no-color", "~/taint-on-upgrade-test/")
			Expect(err).NotTo(HaveOccurred())

			By("Checking plan predicts reinstall for dependent tools")
			Expect(output).To(ContainSubstring("reinstall]"))
			Expect(output).To(ContainSubstring("1 to reinstall"))
		})

		It("upgrades runtime and taints dependent tools", func() {
			By("Applying taint-enabled upgrade config")
			output, err := ExecApply(testExec, "~/taint-on-upgrade-test/")
			Expect(err).NotTo(HaveOccurred())

			By("Checking apply summary shows reinstalled tools")
			Expect(output).To(ContainSubstring("Reinstalled:"))

			By("Verifying new runtime version is installed")
			goOutput, err := testExec.ExecBash("GOTOOLCHAIN=local ~/go/bin/go version")
			Expect(err).NotTo(HaveOccurred())
			Expect(goOutput).To(ContainSubstring("go" + versions.GoVersionUpgrade))

			By("Verifying gopls was reinstalled and still works")
			goplsOutput, err := testExec.ExecBash("~/go/bin/gopls version")
			Expect(err).NotTo(HaveOccurred())
			Expect(goplsOutput).To(ContainSubstring("golang.org/x/tools/gopls"))
		})

		It("is idempotent after taint upgrade", func() {
			By("Running tomei apply again")
			output, err := ExecApply(testExec, "~/taint-on-upgrade-test/")
			Expect(err).NotTo(HaveOccurred())

			By("Checking no reinstalls on second apply")
			Expect(output).NotTo(ContainSubstring("Reinstalled:"))
		})
	})

	AfterAll(func() {
		By("Cleaning up taint-on-upgrade-test state")
		// Restore manifests for potential re-runs
		_, _ = testExec.ExecBash("for f in ~/taint-on-upgrade-test/*.old; do mv \"$f\" \"${f%.old}\"; done 2>/dev/null")
		// Clean up installed resources
		_, _ = testExec.ExecBash("rm -rf ~/.local/share/tomei/runtimes/go ~/.local/share/tomei/tools/gopls")
		_, _ = testExec.ExecBash("rm -f ~/go/bin/go ~/go/bin/gofmt ~/go/bin/gopls")
	})
}
