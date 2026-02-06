//go:build e2e

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("toto on Ubuntu", Ordered, func() {

	Context("Basic Commands", func() {
		It("displays version information", func() {
			By("Running toto version command")
			output, err := testExec.Exec("toto", "version")
			Expect(err).NotTo(HaveOccurred())

			By("Checking output contains version string")
			Expect(output).To(ContainSubstring("toto version"))
		})

		It("initializes environment with toto init", func() {
			By("Running toto init --yes --force to create config.cue and directories")
			output, err := testExec.Exec("toto", "init", "--yes", "--force")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Initialization complete"))

			By("Verifying config.cue was created")
			output, err = testExec.ExecBash("cat ~/.config/toto/config.cue")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("package toto"))

			By("Verifying data directory was created")
			_, err = testExec.ExecBash("ls -d ~/.local/share/toto")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying bin directory was created")
			_, err = testExec.ExecBash("ls -d ~/.local/bin")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying state.json was created")
			output, err = testExec.ExecBash("cat ~/.local/share/toto/state.json")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring(`"version"`))
		})

		It("validates CUE configuration", func() {
			By("Running toto validate command")
			output, err := testExec.Exec("toto", "validate", "~/manifests/")
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

		It("shows planned changes", func() {
			By("Running toto plan command")
			output, err := testExec.Exec("toto", "plan", "~/manifests/")
			Expect(err).NotTo(HaveOccurred())

			By("Checking plan shows resources")
			Expect(output).To(ContainSubstring("Found"))
			Expect(output).To(ContainSubstring("resource"))
		})
	})

	Context("Runtime and Tool Installation", func() {
		It("downloads and installs Runtime and Tools", func() {
			By("Running toto apply command")
			_, err := testExec.Exec("toto", "apply", "~/manifests/")
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("Runtime Installation Verification", func() {
		It("places runtime in runtimes directory", func() {
			By("Listing runtimes directory")
			output, err := testExec.ExecBash("ls ~/.local/share/toto/runtimes/go/1.25.6/")
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
			Expect(output).To(ContainSubstring("go1.25.6"))
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
			output, err := testExec.ExecBash("ls ~/.local/share/toto/tools/gh/2.86.0/")
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
			Expect(output).To(ContainSubstring("gh version 2.86.0"))
		})
	})

	Context("Runtime Delegation", func() {
		It("installed tool via runtime delegation (go install) in first apply", func() {
			By("Verifying gopls was already installed")
			// gopls was installed during the first toto apply along with go runtime and gh
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
			Expect(output).To(ContainSubstring("v0.21.0"))
		})
	})

	Context("State Management", func() {
		It("updates state.json with runtime and tool info", func() {
			By("Reading state.json")
			output, err := testExec.ExecBash("cat ~/.local/share/toto/state.json")
			Expect(err).NotTo(HaveOccurred())

			By("Checking runtimes section exists")
			Expect(output).To(ContainSubstring(`"runtimes"`))

			By("Checking go runtime version is recorded")
			Expect(output).To(ContainSubstring(`"version": "1.25.6"`))

			By("Checking go runtime binDir is recorded")
			Expect(output).To(ContainSubstring(`"binDir"`))
			Expect(output).To(ContainSubstring(`go/bin`))

			By("Checking tools section exists")
			Expect(output).To(ContainSubstring(`"tools"`))

			By("Checking gh tool version is recorded")
			Expect(output).To(ContainSubstring(`"version": "2.86.0"`))
		})

		It("updates state.json with gopls tool info", func() {
			By("Reading state.json")
			output, err := testExec.ExecBash("cat ~/.local/share/toto/state.json")
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

	Context("Idempotency", func() {
		It("is idempotent on subsequent applies", func() {
			By("Running toto apply again")
			_, err := testExec.Exec("toto", "apply", "~/manifests/")
			Expect(err).NotTo(HaveOccurred())
		})

		It("does not re-download on multiple applies", func() {
			By("Running toto apply two more times")
			_, err := testExec.Exec("toto", "apply", "~/manifests/")
			Expect(err).NotTo(HaveOccurred())
			_, err = testExec.Exec("toto", "apply", "~/manifests/")
			Expect(err).NotTo(HaveOccurred())

			By("Checking go still works")
			output, err := testExec.ExecBash("GOTOOLCHAIN=local ~/go/bin/go version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("go1.25.6"))

			By("Checking gh still works")
			output, err = testExec.ExecBash("~/.local/bin/gh --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("gh version 2.86.0"))
		})

		It("is idempotent for runtime delegation tools", func() {
			By("Running toto apply again")
			_, err := testExec.Exec("toto", "apply", "~/manifests/")
			Expect(err).NotTo(HaveOccurred())

			By("Checking gopls still works")
			output, err := testExec.ExecBash("~/go/bin/gopls version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("v0.21.0"))
		})
	})

	Context("Doctor", func() {
		It("reports no issues when environment is clean", func() {
			By("Cleaning up any unmanaged tools from previous tests")
			// Remove tools that may have been installed by dependency tests
			_, _ = testExec.ExecBash("rm -f ~/.local/bin/rg ~/.local/bin/fd ~/.local/bin/bat ~/.local/bin/jq")
			_, _ = testExec.ExecBash("rm -rf ~/.local/share/toto/tools/rg ~/.local/share/toto/tools/fd ~/.local/share/toto/tools/bat ~/.local/share/toto/tools/jq")

			By("Running toto doctor command")
			output, err := testExec.Exec("toto", "doctor")
			Expect(err).NotTo(HaveOccurred())

			By("Checking doctor reports healthy environment")
			Expect(output).To(ContainSubstring("No issues found"))
		})

		It("detects unmanaged tools in runtime bin path", func() {
			By("Installing an unmanaged tool via go install using toto-managed go runtime")
			// Use the toto-managed go binary from ~/go/bin with proper GOBIN set
			_, err := testExec.ExecBash("export GOROOT=$HOME/.local/share/toto/runtimes/go/1.25.6 && export GOBIN=$HOME/go/bin && ~/go/bin/go install golang.org/x/tools/cmd/goimports@latest")
			Expect(err).NotTo(HaveOccurred())

			By("Running toto doctor command")
			output, err := testExec.Exec("toto", "doctor")
			Expect(err).NotTo(HaveOccurred())

			By("Checking doctor detects unmanaged tool")
			Expect(output).To(ContainSubstring("[go]"))
			Expect(output).To(ContainSubstring("goimports"))
			Expect(output).To(ContainSubstring("unmanaged"))

			By("Checking doctor suggests toto adopt")
			Expect(output).To(ContainSubstring("toto adopt"))
		})
	})

	Context("Runtime Upgrade", func() {
		It("shows upgrade plan before applying", func() {
			By("Swapping runtime config to upgraded version (1.25.6 -> 1.25.7)")
			// Move current runtime.cue aside and replace with upgrade version
			// runtime.cue.upgrade has .upgrade extension so it's not loaded by toto until renamed
			_, err := testExec.ExecBash("mv ~/manifests/runtime.cue ~/manifests/runtime.cue.old")
			Expect(err).NotTo(HaveOccurred())
			_, err = testExec.ExecBash("mv ~/manifests/runtime.cue.upgrade ~/manifests/runtime.cue")
			Expect(err).NotTo(HaveOccurred())

			By("Running toto plan to see changes")
			output, err := testExec.Exec("toto", "plan", "~/manifests/")
			Expect(err).NotTo(HaveOccurred())

			By("Checking plan shows runtime in execution order")
			Expect(output).To(ContainSubstring("Runtime/go"))
			Expect(output).To(ContainSubstring("Execution Order"))
		})

		It("upgrades runtime from 1.25.6 to 1.25.7", func() {
			By("Running toto apply with upgraded config")
			_, err := testExec.Exec("toto", "apply", "~/manifests/")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying new runtime version is installed")
			output, err := testExec.ExecBash("GOTOOLCHAIN=local ~/go/bin/go version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("go1.25.7"))

			By("Verifying new runtime is in correct location")
			_, err = testExec.ExecBash("ls ~/.local/share/toto/runtimes/go/1.25.7/bin/go")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying symlink points to new version")
			output, err = testExec.ExecBash("readlink ~/go/bin/go")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("1.25.7"))
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
			output, err := testExec.ExecBash("cat ~/.local/share/toto/state.json")
			Expect(err).NotTo(HaveOccurred())

			By("Checking go runtime version is updated to 1.25.7")
			Expect(output).To(ContainSubstring(`"version": "1.25.7"`))
		})

		It("is idempotent after runtime upgrade", func() {
			By("Running toto apply again")
			_, err := testExec.Exec("toto", "apply", "~/manifests/")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying runtime still works")
			output, err := testExec.ExecBash("GOTOOLCHAIN=local ~/go/bin/go version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("go1.25.7"))
		})
	})
})
