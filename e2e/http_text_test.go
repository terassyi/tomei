//go:build e2e

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func httpTextTests() {

	BeforeAll(func() {
		By("Resetting state for http-text tests")
		_, _ = testExec.Exec("tomei", "init", "--yes", "--force")
		_, _ = testExec.ExecBash(`echo '{"runtimes":{},"tools":{},"installers":{},"installerRepositories":{}}' > ~/.local/share/tomei/state.json`)

		By("Cleaning up leftover artifacts")
		_, _ = testExec.ExecBash("rm -rf /tmp/http-text-rt-bin /tmp/http-text-server /tmp/mock-rt-build ~/.local/share/tomei/runtimes/http-text-rt")

		By("Creating mock runtime archive and version endpoint")
		_, err := testExec.ExecBash(`
			mkdir -p /tmp/http-text-server/archive /tmp/mock-rt-build/http-text-rt/bin /tmp/http-text-rt-bin &&
			echo "v1.0.0" > /tmp/http-text-server/version.txt &&
			printf '#!/bin/bash\necho "http-text-rt v1.0.0"' > /tmp/mock-rt-build/http-text-rt/bin/http-text-rt &&
			chmod +x /tmp/mock-rt-build/http-text-rt/bin/http-text-rt &&
			tar czf /tmp/http-text-server/archive/http-text-rt-v1.0.0.tar.gz -C /tmp/mock-rt-build http-text-rt
		`)
		Expect(err).NotTo(HaveOccurred())

		By("Starting Python HTTP server on port 18888")
		_, err = testExec.ExecBash("cd /tmp/http-text-server && python3 -m http.server 18888 > /tmp/http-text-server/server.log 2>&1 & echo $! > /tmp/http-text-server/server.pid && sleep 1")
		Expect(err).NotTo(HaveOccurred())

		By("Verifying HTTP server is running")
		output, err := testExec.ExecBash("curl -sf http://localhost:18888/version.txt")
		Expect(err).NotTo(HaveOccurred())
		Expect(output).To(ContainSubstring("v1.0.0"))
	})

	Context("Alias Version Resolution via http-text", func() {
		It("validates http-text-test manifest", func() {
			By("Running tomei validate")
			output, err := testExec.Exec("tomei", "validate", "~/http-text-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Validation successful"))
			Expect(output).To(ContainSubstring("Runtime/http-text-rt"))
		})

		It("resolves version from HTTP endpoint and installs runtime", func() {
			By("Applying http-text-test manifests (version: latest)")
			output, err := ExecApply(testExec, "~/http-text-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("http-text-rt"))

			By("Verifying runtime binary is executable")
			output, err = testExec.ExecBash("/tmp/http-text-rt-bin/http-text-rt")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("http-text-rt v1.0.0"))

			By("Verifying state records resolved version with alias kind")
			output, err = testExec.Exec("tomei", "get", "runtimes", "-o", "json")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring(`"1.0.0"`))
			Expect(output).To(ContainSubstring("alias"))
		})

		It("is idempotent after alias resolution", func() {
			output, err := ExecApply(testExec, "~/http-text-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("No changes to apply"))
		})
	})

	Context("Exact Version Skip", func() {
		BeforeAll(func() {
			By("Resetting state and runtime artifacts")
			_, _ = testExec.ExecBash(`echo '{"runtimes":{},"tools":{},"installers":{},"installerRepositories":{}}' > ~/.local/share/tomei/state.json`)
			_, _ = testExec.ExecBash("rm -rf ~/.local/share/tomei/runtimes/http-text-rt /tmp/http-text-rt-bin/*")

			By("Changing version endpoint to v2.0.0 to prove resolution is skipped")
			_, err := testExec.ExecBash("echo 'v2.0.0' > /tmp/http-text-server/version.txt")
			Expect(err).NotTo(HaveOccurred())

			By("Swapping to exact version manifest")
			_, err = testExec.ExecBash("mv ~/http-text-test/runtime.cue ~/http-text-test/runtime.cue.alias")
			Expect(err).NotTo(HaveOccurred())
			_, err = testExec.ExecBash("mv ~/http-text-test/runtime.cue.exact ~/http-text-test/runtime.cue")
			Expect(err).NotTo(HaveOccurred())
		})

		It("skips HTTP resolution when exact version is specified", func() {
			By("Applying with exact version 1.0.0 (server now advertises v2.0.0)")
			output, err := ExecApply(testExec, "~/http-text-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("http-text-rt"))

			By("Verifying installed version is 1.0.0 (not 2.0.0 from server)")
			output, err = testExec.ExecBash("/tmp/http-text-rt-bin/http-text-rt")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("http-text-rt v1.0.0"))

			By("Verifying state records exact version kind")
			output, err = testExec.Exec("tomei", "get", "runtimes", "-o", "json")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring(`"1.0.0"`))
			Expect(output).To(ContainSubstring("exact"))
		})

		It("is idempotent after exact version install", func() {
			output, err := ExecApply(testExec, "~/http-text-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("No changes to apply"))
		})
	})

	AfterAll(func() {
		By("Stopping HTTP server")
		_, _ = testExec.ExecBash("kill $(cat /tmp/http-text-server/server.pid) 2>/dev/null")
		By("Restoring manifest files")
		_, _ = testExec.ExecBash("for f in ~/http-text-test/*.alias; do mv \"$f\" \"${f%.alias}\"; done 2>/dev/null")
		By("Cleaning up artifacts")
		_, _ = testExec.ExecBash("rm -rf /tmp/http-text-rt-bin /tmp/http-text-server /tmp/mock-rt-build ~/.local/share/tomei/runtimes/http-text-rt")
	})
}
