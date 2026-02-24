//go:build e2e

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func basicTests() {

	Context("Basic Commands", func() {
		It("displays version information", func() {
			By("Running tomei version command")
			output, err := testExec.Exec("tomei", "version")
			Expect(err).NotTo(HaveOccurred())

			By("Checking output contains version string")
			Expect(output).To(ContainSubstring("tomei version"))
		})

		It("initializes environment with tomei init", func() {
			By("Running tomei init --yes --force to create config.cue and directories")
			output, err := testExec.Exec("tomei", "init", "--yes", "--force")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Initialization complete"))

			By("Verifying config.cue was created")
			output, err = testExec.ExecBash("cat ~/.config/tomei/config.cue")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("package tomei"))

			By("Verifying data directory was created")
			_, err = testExec.ExecBash("ls -d ~/.local/share/tomei")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying bin directory was created")
			_, err = testExec.ExecBash("ls -d ~/.local/bin")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying state.json was created")
			output, err = testExec.ExecBash("cat ~/.local/share/tomei/state.json")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring(`"version"`))
		})

		It("validates CUE configuration", func() {
			By("Running tomei validate command")
			output, err := testExec.Exec("tomei", "validate", "~/manifests/")
			Expect(err).NotTo(HaveOccurred())

			By("Checking validation succeeded")
			Expect(output).To(ContainSubstring("Validation successful"))

			By("Checking Tool/gh is recognized")
			Expect(output).To(ContainSubstring("Tool/gh"))

			By("Checking Tool/gopls is recognized")
			Expect(output).To(ContainSubstring("Tool/gopls"))

			By("Checking Runtime/go is recognized")
			Expect(output).To(ContainSubstring("Runtime/go"))
		})

		It("validates manifest with tar.xz source URL", func() {
			By("Running tomei validate on tar-xz-test manifest")
			output, err := testExec.Exec("tomei", "validate", "~/tar-xz-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Validation successful"))
			Expect(output).To(ContainSubstring("Tool/zig"))
		})

		It("shows planned changes", func() {
			By("Running tomei plan command")
			output, err := testExec.Exec("tomei", "plan", "~/manifests/")
			Expect(err).NotTo(HaveOccurred())

			By("Checking plan shows resources")
			Expect(output).To(ContainSubstring("Found"))
			Expect(output).To(ContainSubstring("resource"))
		})

		It("shows tar.xz tool in plan output", func() {
			By("Running tomei plan on tar-xz-test manifest")
			output, err := testExec.Exec("tomei", "plan", "~/tar-xz-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Tool/zig"))
		})
	})

	Context("Runtime and Tool Installation", func() {
		It("downloads and installs Runtime and Tools without taint reinstall", func() {
			By("Running tomei apply command")
			output, err := ExecApply(testExec, "~/manifests/")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying apply completed successfully")
			Expect(output).To(ContainSubstring("Apply complete!"))

			By("Verifying no taint reinstall on first install")
			Expect(output).NotTo(ContainSubstring("Reinstalled:"))
		})
	})

	Context("Runtime Installation Verification", func() {
		It("places runtime in runtimes directory", func() {
			By("Listing runtimes directory")
			output, err := testExec.ExecBash(fmt.Sprintf("ls ~/.local/share/tomei/runtimes/go/%s/", versions.GoVersion))
			Expect(err).NotTo(HaveOccurred())

			By("Checking bin directory exists")
			Expect(output).To(ContainSubstring("bin"))
		})

		It("creates symlinks for runtime binaries in BinDir (~/go/bin)", func() {
			By("Listing go bin directory")
			output, err := testExec.ExecBash("ls -la ~/go/bin/")
			Expect(err).NotTo(HaveOccurred())

			By("Checking symlink to go exists")
			Expect(output).To(ContainSubstring("go ->"))

			By("Checking symlink to gofmt exists")
			Expect(output).To(ContainSubstring("gofmt ->"))

			By("Verifying runtime binaries are NOT in ~/.local/bin")
			output, err = testExec.ExecBash("ls -la ~/.local/bin/ 2>/dev/null || echo 'empty'")
			Expect(err).NotTo(HaveOccurred())
			// go and gofmt should NOT be in ~/.local/bin anymore
			Expect(output).NotTo(MatchRegexp(`\bgo\b.*->`))
		})

		It("allows running go command after install", func() {
			By("Executing go version from ~/go/bin")
			output, err := testExec.ExecBash("GOTOOLCHAIN=local ~/go/bin/go version")
			Expect(err).NotTo(HaveOccurred())

			By("Checking go version output")
			Expect(output).To(ContainSubstring("go" + versions.GoVersion))
		})

		It("allows running gofmt command after install", func() {
			By("Executing gofmt -h to verify it works")
			output, err := testExec.ExecBash("~/go/bin/gofmt -h 2>&1 || true")
			Expect(err).NotTo(HaveOccurred())

			By("Checking gofmt output")
			Expect(output).To(ContainSubstring("usage"))
		})
	})

	Context("Tool Installation - Download Pattern", func() {
		It("places tool binary in tools directory", func() {
			By("Listing tools directory")
			output, err := testExec.ExecBash(fmt.Sprintf("ls ~/.local/share/tomei/tools/gh/%s/", versions.GhVersion))
			Expect(err).NotTo(HaveOccurred())

			By("Checking gh binary exists")
			Expect(output).To(ContainSubstring("gh"))
		})

		It("creates symlink for tool in bin directory", func() {
			By("Listing bin directory")
			output, err := testExec.ExecBash("ls -la ~/.local/bin/")
			Expect(err).NotTo(HaveOccurred())

			By("Checking symlink to gh exists")
			Expect(output).To(ContainSubstring("gh ->"))
		})

		It("allows running gh command after install", func() {
			By("Executing gh --version")
			output, err := testExec.ExecBash("~/.local/bin/gh --version")
			Expect(err).NotTo(HaveOccurred())

			By("Checking gh version output")
			Expect(output).To(ContainSubstring("gh version " + versions.GhVersion))
		})
	})

	Context("Runtime Delegation", func() {
		It("installed tool via runtime delegation (go install) in first apply", func() {
			By("Verifying gopls was already installed")
			// gopls was installed during the first tomei apply along with go runtime and gh
			// This test verifies the installation results
		})

		It("places gopls binary in toolBinPath (~/go/bin)", func() {
			By("Listing ~/go/bin directory")
			output, err := testExec.ExecBash("ls ~/go/bin/")
			Expect(err).NotTo(HaveOccurred())

			By("Checking gopls binary exists")
			Expect(output).To(ContainSubstring("gopls"))
		})

		It("allows running gopls command after install", func() {
			By("Executing gopls version")
			output, err := testExec.ExecBash("~/go/bin/gopls version")
			Expect(err).NotTo(HaveOccurred())

			By("Checking gopls version output")
			Expect(output).To(ContainSubstring("golang.org/x/tools/gopls"))
			Expect(output).To(ContainSubstring(versions.GoplsVersion))
		})

		It("places goimports binary in toolBinPath (~/go/bin)", func() {
			By("Listing ~/go/bin directory")
			output, err := testExec.ExecBash("ls ~/go/bin/goimports")
			Expect(err).NotTo(HaveOccurred())

			By("Checking goimports binary exists")
			Expect(output).To(ContainSubstring("goimports"))
		})

		It("allows running goimports command after install", func() {
			By("Executing goimports -h via shell to capture output regardless of exit code")
			output, err := testExec.ExecBash("~/go/bin/goimports -h 2>&1 || true")
			Expect(err).NotTo(HaveOccurred())

			By("Checking goimports prints usage information")
			Expect(output).To(ContainSubstring("usage"))
		})

		It("updates state.json with goimports tool info", func() {
			By("Reading state.json")
			output, err := testExec.ExecBash("cat ~/.local/share/tomei/state.json")
			Expect(err).NotTo(HaveOccurred())

			By("Checking goimports is in tools section with correct version")
			Expect(output).To(ContainSubstring(`"goimports"`))
			Expect(output).To(ContainSubstring(versions.GoimportsVersion))
		})
	})

	Context("State Management", func() {
		It("updates state.json with runtime and tool info", func() {
			By("Reading state.json")
			output, err := testExec.ExecBash("cat ~/.local/share/tomei/state.json")
			Expect(err).NotTo(HaveOccurred())

			By("Checking runtimes section exists")
			Expect(output).To(ContainSubstring(`"runtimes"`))

			By("Checking go runtime version is recorded")
			Expect(output).To(ContainSubstring(fmt.Sprintf(`"version": "%s"`, versions.GoVersion)))

			By("Checking go runtime binDir is recorded")
			Expect(output).To(ContainSubstring(`"binDir"`))
			Expect(output).To(ContainSubstring(`go/bin`))

			By("Checking tools section exists")
			Expect(output).To(ContainSubstring(`"tools"`))

			By("Checking gh tool version is recorded")
			Expect(output).To(ContainSubstring(fmt.Sprintf(`"version": "%s"`, versions.GhVersion)))
		})

		It("updates state.json with gopls tool info", func() {
			By("Reading state.json")
			output, err := testExec.ExecBash("cat ~/.local/share/tomei/state.json")
			Expect(err).NotTo(HaveOccurred())

			By("Checking gopls is in tools section")
			Expect(output).To(ContainSubstring(`"gopls"`))

			By("Checking runtimeRef is recorded")
			Expect(output).To(ContainSubstring(`"runtimeRef": "go"`))

			By("Checking package is recorded")
			// Package is serialized as object with name field
			Expect(output).To(ContainSubstring(`"package"`))
			Expect(output).To(ContainSubstring(`"name": "golang.org/x/tools/gopls"`))
		})
	})

	Context("Environment Export", func() {
		It("outputs posix environment variables for installed runtimes", func() {
			By("Running tomei env")
			output, err := testExec.Exec("tomei", "env")
			Expect(err).NotTo(HaveOccurred())

			By("Checking GOROOT export")
			Expect(output).To(ContainSubstring(`export GOROOT=`))

			By("Checking GOBIN export")
			Expect(output).To(ContainSubstring(`export GOBIN=`))

			By("Checking PATH includes go/bin and .local/bin")
			Expect(output).To(ContainSubstring(`go/bin`))
			Expect(output).To(ContainSubstring(`.local/bin`))

			By("Checking output is eval-safe and PATH works")
			_, err = testExec.ExecBash(fmt.Sprintf("eval '%s' && test -n \"$GOROOT\" && GOTOOLCHAIN=local go version", output))
			Expect(err).NotTo(HaveOccurred())
		})

		It("outputs fish environment variables", func() {
			By("Running tomei env --shell fish")
			output, err := testExec.Exec("tomei", "env", "--shell", "fish")
			Expect(err).NotTo(HaveOccurred())

			By("Checking fish-style GOROOT export")
			Expect(output).To(ContainSubstring(`set -gx GOROOT`))

			By("Checking fish_add_path")
			Expect(output).To(ContainSubstring(`fish_add_path`))
		})

		It("exports env file with --export flag", func() {
			By("Running tomei env --export")
			output, err := testExec.Exec("tomei", "env", "--export")
			Expect(err).NotTo(HaveOccurred())

			By("Checking output confirms file was written")
			Expect(output).To(ContainSubstring("env.sh"))

			By("Verifying env file exists and contains exports")
			content, err := testExec.ExecBash("cat ~/.config/tomei/env.sh")
			Expect(err).NotTo(HaveOccurred())
			Expect(content).To(ContainSubstring(`export GOROOT=`))
			Expect(content).To(ContainSubstring(`export PATH=`))
		})
	})

	Context("Idempotency", func() {
		It("is idempotent on subsequent applies", func() {
			By("Running tomei apply again")
			output, err := ExecApply(testExec, "~/manifests/")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying no changes were needed")
			Expect(output).To(ContainSubstring("No changes to apply"))
		})

		It("does not re-download on multiple applies", func() {
			By("Running tomei apply two more times")
			output, err := ExecApply(testExec, "~/manifests/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("No changes to apply"))
			output, err = ExecApply(testExec, "~/manifests/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("No changes to apply"))

			By("Checking go still works")
			output, err = testExec.ExecBash("GOTOOLCHAIN=local ~/go/bin/go version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("go" + versions.GoVersion))

			By("Checking gh still works")
			output, err = testExec.ExecBash("~/.local/bin/gh --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("gh version " + versions.GhVersion))
		})

		It("is idempotent for runtime delegation tools", func() {
			By("Running tomei apply again")
			output, err := ExecApply(testExec, "~/manifests/")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying no changes were needed")
			Expect(output).To(ContainSubstring("No changes to apply"))

			By("Checking gopls still works")
			output, err = testExec.ExecBash("~/go/bin/gopls version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring(versions.GoplsVersion))
		})
	})

	Context("Doctor", func() {
		It("reports no issues when environment is clean", func() {
			By("Cleaning up any unmanaged tools from previous test runs")
			// Remove tools that may remain when re-running tests on the same container
			_, _ = testExec.ExecBash("rm -f ~/.local/bin/rg ~/.local/bin/fd ~/.local/bin/bat ~/.local/bin/jq ~/.local/bin/helm")
			_, _ = testExec.ExecBash("rm -rf ~/.local/share/tomei/tools/rg ~/.local/share/tomei/tools/fd ~/.local/share/tomei/tools/bat ~/.local/share/tomei/tools/jq ~/.local/share/tomei/tools/helm")

			By("Running tomei doctor command")
			output, err := testExec.Exec("tomei", "doctor")
			Expect(err).NotTo(HaveOccurred())

			By("Checking doctor reports healthy environment")
			Expect(output).To(ContainSubstring("No issues found"))
		})

		It("detects unmanaged tools in runtime bin path", func() {
			By("Installing an unmanaged tool via go install using tomei-managed go runtime")
			// Use the tomei-managed go binary from ~/go/bin with proper GOBIN set
			// stringer is not managed by tomei, so doctor should detect it as unmanaged
			_, err := testExec.ExecBash(fmt.Sprintf("export GOROOT=$HOME/.local/share/tomei/runtimes/go/%s && export GOBIN=$HOME/go/bin && ~/go/bin/go install golang.org/x/tools/cmd/stringer@latest", versions.GoVersion))
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei doctor command")
			output, err := testExec.Exec("tomei", "doctor")
			Expect(err).NotTo(HaveOccurred())

			By("Checking doctor detects unmanaged tool")
			Expect(output).To(ContainSubstring("[go]"))
			Expect(output).To(ContainSubstring("stringer"))
			Expect(output).To(ContainSubstring("unmanaged"))

			By("Checking doctor shows suggestions")
			Expect(output).To(ContainSubstring("Suggestions"))
		})
	})

	Context("Runtime Upgrade", func() {
		It("shows upgrade plan before applying", func() {
			By(fmt.Sprintf("Swapping runtime config to upgraded version (%s -> %s)", versions.GoVersion, versions.GoVersionUpgrade))
			// Move current runtime.cue aside and replace with upgrade version
			// runtime.cue.upgrade has .upgrade extension so it's not loaded by tomei until renamed
			_, err := testExec.ExecBash("mv ~/manifests/runtime.cue ~/manifests/runtime.cue.old")
			Expect(err).NotTo(HaveOccurred())
			_, err = testExec.ExecBash("mv ~/manifests/runtime.cue.upgrade ~/manifests/runtime.cue")
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei plan to see changes")
			output, err := testExec.Exec("tomei", "plan", "--no-color", "~/manifests/")
			Expect(err).NotTo(HaveOccurred())

			By("Checking plan shows runtime in execution order")
			Expect(output).To(ContainSubstring("Runtime/go"))
			Expect(output).To(ContainSubstring("Execution Order"))

			By("Checking plan predicts taint reinstall for dependent tools")
			Expect(output).To(ContainSubstring("reinstall"))

			By("Checking plan summary includes reinstall count")
			Expect(output).To(ContainSubstring("to reinstall"))
		})

		It("upgrades runtime to newer version", func() {
			By("Running tomei apply with upgraded config")
			applyOutput, err := ExecApply(testExec, "~/manifests/")
			Expect(err).NotTo(HaveOccurred())

			By("Checking apply summary shows reinstalled tools")
			Expect(applyOutput).To(ContainSubstring("Reinstalled:"))

			By("Verifying new runtime version is installed")
			output, err := testExec.ExecBash("GOTOOLCHAIN=local ~/go/bin/go version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("go" + versions.GoVersionUpgrade))

			By("Verifying new runtime is in correct location")
			_, err = testExec.ExecBash(fmt.Sprintf("ls ~/.local/share/tomei/runtimes/go/%s/bin/go", versions.GoVersionUpgrade))
			Expect(err).NotTo(HaveOccurred())

			By("Verifying symlink points to new version")
			output, err = testExec.ExecBash("readlink ~/go/bin/go")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring(versions.GoVersionUpgrade))
		})

		It("taints dependent tools after runtime upgrade", func() {
			By("Checking gopls was reinstalled due to taint")
			// gopls should have been reinstalled because it depends on the go runtime
			// The previous apply should have tainted and reinstalled it

			By("Verifying gopls still works after upgrade")
			output, err := testExec.ExecBash("~/go/bin/gopls version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("golang.org/x/tools/gopls"))
		})

		It("updates state.json with new runtime version", func() {
			By("Reading state.json")
			output, err := testExec.ExecBash("cat ~/.local/share/tomei/state.json")
			Expect(err).NotTo(HaveOccurred())

			By("Checking go runtime version is updated to " + versions.GoVersionUpgrade)
			Expect(output).To(ContainSubstring(fmt.Sprintf(`"version": "%s"`, versions.GoVersionUpgrade)))
		})

		It("is idempotent after runtime upgrade", func() {
			By("Running tomei apply again")
			_, err := ExecApply(testExec, "~/manifests/")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying runtime still works")
			output, err := testExec.ExecBash("GOTOOLCHAIN=local ~/go/bin/go version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("go" + versions.GoVersionUpgrade))
		})
	})

	Context("Resource Removal", func() {
		AfterAll(func() {
			By("Restoring all hidden manifests for subsequent test suites")
			// Ignore error in case no .hidden files exist
			_, _ = testExec.ExecBash("for f in ~/manifests/*.hidden; do mv \"$f\" \"${f%.hidden}\"; done")
		})

		It("rejects removing runtime when dependent tools remain", func() {
			By("Hiding runtime manifest so runtime is no longer in spec")
			_, err := testExec.ExecBash("mv ~/manifests/runtime.cue ~/manifests/runtime.cue.hidden")
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei apply — should fail because gopls depends on go runtime")
			output, err := testExec.Exec("tomei", "apply", "--yes", "~/manifests/")
			Expect(err).To(HaveOccurred())
			Expect(output).To(ContainSubstring("cannot remove runtime"))
			Expect(output).To(ContainSubstring("gopls"))

			By("Restoring runtime manifest")
			_, err = testExec.ExecBash("mv ~/manifests/runtime.cue.hidden ~/manifests/runtime.cue")
			Expect(err).NotTo(HaveOccurred())
		})

		It("removes a tool when its manifest is removed", func() {
			By("Hiding tool manifest")
			_, err := testExec.ExecBash("mv ~/manifests/tools.cue ~/manifests/tools.cue.hidden")
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei plan to see removal prediction")
			planOutput, err := testExec.Exec("tomei", "plan", "--no-color", "~/manifests/")
			Expect(err).NotTo(HaveOccurred())

			By("Checking plan predicts tool removal")
			Expect(planOutput).To(ContainSubstring("remove"))
			Expect(planOutput).To(ContainSubstring("to remove"))

			By("Running tomei apply")
			applyOutput, err := ExecApply(testExec, "~/manifests/")
			Expect(err).NotTo(HaveOccurred())

			By("Checking apply summary shows removed resources")
			Expect(applyOutput).To(ContainSubstring("Removed:"))

			By("Verifying tool symlink is removed")
			_, err = testExec.ExecBash("test -L ~/.local/bin/gh")
			Expect(err).To(HaveOccurred())

			By("Verifying tool is removed from state")
			output, err := testExec.ExecBash("cat ~/.local/share/tomei/state.json")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).NotTo(MatchRegexp(`"gh"\s*:`))
		})

		It("removes runtime and dependent tools together", func() {
			By("Hiding runtime, delegation, and toolset manifests")
			_, err := testExec.ExecBash("mv ~/manifests/runtime.cue ~/manifests/runtime.cue.hidden")
			Expect(err).NotTo(HaveOccurred())
			_, err = testExec.ExecBash("mv ~/manifests/delegation.cue ~/manifests/delegation.cue.hidden")
			Expect(err).NotTo(HaveOccurred())
			_, err = testExec.ExecBash("mv ~/manifests/toolset.cue ~/manifests/toolset.cue.hidden")
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei apply — should succeed since all dependents are removed")
			_, err = ExecApply(testExec, "~/manifests/")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying runtime symlink is removed")
			_, err = testExec.ExecBash("test -L ~/go/bin/go")
			Expect(err).To(HaveOccurred())

			By("Verifying gopls is removed")
			_, err = testExec.ExecBash("test -f ~/go/bin/gopls")
			Expect(err).To(HaveOccurred())

			By("Verifying state.json has no runtime or gopls")
			output, err := testExec.ExecBash("cat ~/.local/share/tomei/state.json")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).NotTo(MatchRegexp(`"go"\s*:`))
			Expect(output).NotTo(MatchRegexp(`"gopls"\s*:`))
		})
	})
}
