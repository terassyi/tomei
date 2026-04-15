//go:build e2e

package e2e

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func privilegedTests() {

	BeforeAll(func() {
		By("Resetting state for privileged tests")
		_, _ = testExec.Exec("tomei", "init", "--yes", "--force")
		_, _ = testExec.ExecBash(`echo '{"runtimes":{},"tools":{},"installers":{},"installerRepositories":{}}' > ~/.local/share/tomei/state.json`)
		// Clean up any leftover artifacts
		_, _ = testExec.ExecBash("rm -rf /tmp/tomei-privileged-test /tmp/tomei-normal-test")
	})

	Context("Validate", func() {
		It("validates privileged-test manifests", func() {
			output, err := testExec.Exec("tomei", "validate", "~/privileged-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Validation successful"))
			Expect(output).To(ContainSubstring("Tool/privileged-tool"))
			Expect(output).To(ContainSubstring("Tool/normal-tool"))
		})
	})

	Context("Plan without --system", func() {
		It("shows privileged tool as skip", func() {
			output, err := testExec.Exec("tomei", "plan", "~/privileged-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("privileged-tool"))
			Expect(output).To(ContainSubstring("skip"))
			// Normal tool should show as install
			Expect(output).To(ContainSubstring("normal-tool"))
			Expect(output).To(ContainSubstring("install"))
		})
	})

	Context("Apply without --system", func() {
		It("skips privileged tool and installs normal tool", func() {
			output, err := ExecApply(testExec, "~/privileged-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("privileged resource(s) skipped"))
		})

		It("does not install privileged tool", func() {
			_, err := testExec.ExecBash("test -f /tmp/tomei-privileged-test/marker")
			Expect(err).To(HaveOccurred(), "privileged tool marker should not exist")
		})

		It("installs normal tool", func() {
			output, err := testExec.ExecBash("cat /tmp/tomei-normal-test/marker")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("installed"))
		})

		It("records only normal tool in state", func() {
			output, err := testExec.Exec("tomei", "get", "tools", "-o", "json")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring(`"normal-tool"`))
			Expect(output).NotTo(ContainSubstring(`"privileged-tool"`))
		})
	})

	Context("Apply with --system", func() {
		BeforeAll(func() {
			// Reset state and artifacts for --system test
			_, _ = testExec.ExecBash(`echo '{"runtimes":{},"tools":{},"installers":{},"installerRepositories":{}}' > ~/.local/share/tomei/state.json`)
			_, _ = testExec.ExecBash("rm -rf /tmp/tomei-privileged-test /tmp/tomei-normal-test")
		})

		It("installs both privileged and normal tools", func() {
			output, err := ExecApply(testExec, "--system", "~/privileged-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("privileged-tool"))
			Expect(output).To(ContainSubstring("normal-tool"))
		})

		It("creates privileged tool marker via sudo", func() {
			// Verify the marker exists and is owned by root (uid 0). Writing
			// to /tmp would succeed even unprivileged, so ownership is the
			// distinguishing signal that sudo was actually used.
			output, err := testExec.ExecBash("cat /tmp/tomei-privileged-test/marker")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("installed"))

			// Portable across GNU coreutils (Linux) and BSD (macOS): ls -n prints
			// the numeric owner UID as the 3rd column. `stat -c` is GNU-only and
			// `stat -f` is BSD-only, so parsing `ls -n` avoids OS branching.
			ownerUID, err := testExec.ExecBash("ls -lan /tmp/tomei-privileged-test/marker | awk '{print $3}'")
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(ownerUID)).To(Equal("0"), "privileged tool marker should be owned by root")
		})

		It("creates normal tool marker without sudo", func() {
			output, err := testExec.ExecBash("cat /tmp/tomei-normal-test/marker")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("installed"))
		})

		It("records both tools in state", func() {
			output, err := testExec.Exec("tomei", "get", "tools", "-o", "json")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring(`"privileged-tool"`))
			Expect(output).To(ContainSubstring(`"normal-tool"`))
		})

		It("records privileged flag in state", func() {
			output, err := testExec.Exec("tomei", "get", "tools", "-o", "json")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring(`"privileged": true`))
		})
	})

	Context("Removal without --system", func() {
		// At this point both tools are installed (from --system apply above).
		// Create a manifest without the privileged tool to trigger removal.
		It("skips removal of privileged tool without --system", func() {
			// Create a temporary manifest with only normal-tool
			_, err := testExec.ExecBash(`mkdir -p /tmp/tomei-removal-test/cue.mod && echo 'module: "tomei.local@v0"
language: version: "v0.9.0"' > /tmp/tomei-removal-test/cue.mod/module.cue && echo 'package tomei
normalTool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "normal-tool"
	spec: commands: {
		install: ["mkdir -p /tmp/tomei-normal-test && echo installed > /tmp/tomei-normal-test/marker"]
		check: ["test -f /tmp/tomei-normal-test/marker"]
		remove: ["rm -rf /tmp/tomei-normal-test"]
		resolveVersion: ["echo 1.0.0"]
	}
}' > /tmp/tomei-removal-test/tools.cue`)
			Expect(err).NotTo(HaveOccurred())

			// Apply without --system; privileged tool should remain in state
			output, err := ExecApply(testExec, "/tmp/tomei-removal-test/")
			Expect(err).NotTo(HaveOccurred())
			_ = output

			// Privileged tool should still be in state (not removed)
			stateOutput, err := testExec.ExecBash("cat ~/.local/share/tomei/state.json")
			Expect(err).NotTo(HaveOccurred())
			Expect(stateOutput).To(ContainSubstring("privileged-tool"))
		})
	})
}
