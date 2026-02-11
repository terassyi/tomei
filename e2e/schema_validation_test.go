//go:build e2e

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func schemaValidationTests() {

	BeforeAll(func() {
		// Ensure tomei is initialized (may already be from Basic tests)
		_, _ = testExec.Exec("tomei", "init", "--yes")
		// Create temp directory for invalid manifests
		_, err := testExec.ExecBash("mkdir -p ~/schema-test")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		_, _ = testExec.ExecBash("rm -rf ~/schema-test")
	})

	Context("tomei init Schema Placement", func() {
		It("places schema.cue in current directory by default", func() {
			By("Verifying schema.cue was created by init in working directory")
			output, err := testExec.ExecBash("cat ~/schema.cue")
			Expect(err).NotTo(HaveOccurred())

			By("Checking schema contains key definitions")
			Expect(output).To(ContainSubstring("package tomei"))
			Expect(output).To(ContainSubstring("#APIVersion"))
			Expect(output).To(ContainSubstring("#Resource"))
			Expect(output).To(ContainSubstring("#HTTPSURL"))
			Expect(output).To(ContainSubstring("#Metadata"))
		})

		It("places schema.cue in custom directory with --schema-dir", func() {
			By("Creating custom schema directory")
			_, err := testExec.ExecBash("mkdir -p ~/custom-schema-dir")
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei init with --schema-dir")
			output, err := testExec.Exec("tomei", "init", "--yes", "--force", "--schema-dir", "~/custom-schema-dir")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Initialization complete"))

			By("Verifying schema.cue exists in custom directory")
			output, err = testExec.ExecBash("cat ~/custom-schema-dir/schema.cue")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("#APIVersion"))
			Expect(output).To(ContainSubstring("#Resource"))

			By("Cleaning up custom directory")
			_, _ = testExec.ExecBash("rm -rf ~/custom-schema-dir")

			By("Re-initializing with default settings for subsequent tests")
			_, err = testExec.Exec("tomei", "init", "--yes", "--force")
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("tomei validate Rejects Invalid Manifests", func() {
		It("rejects wrong apiVersion", func() {
			By("Writing manifest with wrong apiVersion")
			_, err := testExec.ExecBash(`cat > ~/schema-test/wrong-api.cue << 'EOF'
apiVersion: "wrong/v1"
kind:       "Tool"
metadata: name: "test"
spec: {
    installerRef: "download"
    version:      "1.0.0"
}
EOF`)
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei validate")
			output, err := testExec.Exec("tomei", "validate", "~/schema-test/wrong-api.cue")
			Expect(err).To(HaveOccurred())
			Expect(output).To(ContainSubstring("validation failed"))
		})

		It("rejects non-HTTPS URL in source", func() {
			By("Writing manifest with HTTP URL")
			_, err := testExec.ExecBash(`cat > ~/schema-test/http-url.cue << 'EOF'
apiVersion: "tomei.terassyi.net/v1beta1"
kind:       "Tool"
metadata: name: "test"
spec: {
    installerRef: "download"
    version:      "1.0.0"
    source: {
        url: "http://example.com/tool.tar.gz"
    }
}
EOF`)
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei validate")
			output, err := testExec.Exec("tomei", "validate", "~/schema-test/http-url.cue")
			Expect(err).To(HaveOccurred())
			Expect(output).To(ContainSubstring("validation failed"))
		})

		It("rejects invalid metadata name", func() {
			By("Writing manifest with uppercase name")
			_, err := testExec.ExecBash(`cat > ~/schema-test/bad-name.cue << 'EOF'
apiVersion: "tomei.terassyi.net/v1beta1"
kind:       "Tool"
metadata: name: "INVALID_NAME"
spec: {
    installerRef: "download"
    version:      "1.0.0"
}
EOF`)
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei validate")
			output, err := testExec.Exec("tomei", "validate", "~/schema-test/bad-name.cue")
			Expect(err).To(HaveOccurred())
			Expect(output).To(ContainSubstring("validation failed"))
		})

		It("rejects Runtime download type without source", func() {
			By("Writing manifest with download type but no source")
			_, err := testExec.ExecBash(`cat > ~/schema-test/no-source.cue << 'EOF'
apiVersion: "tomei.terassyi.net/v1beta1"
kind:       "Runtime"
metadata: name: "go"
spec: {
    type:        "download"
    version:     "1.25.6"
    toolBinPath: "~/go/bin"
}
EOF`)
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei validate")
			output, err := testExec.Exec("tomei", "validate", "~/schema-test/no-source.cue")
			Expect(err).To(HaveOccurred())
			Expect(output).To(ContainSubstring("validation failed"))
		})

		It("rejects invalid checksum format", func() {
			By("Writing manifest with md5 checksum")
			_, err := testExec.ExecBash(`cat > ~/schema-test/bad-checksum.cue << 'EOF'
apiVersion: "tomei.terassyi.net/v1beta1"
kind:       "Tool"
metadata: name: "test"
spec: {
    installerRef: "download"
    version:      "1.0.0"
    source: {
        url: "https://example.com/tool.tar.gz"
        checksum: {
            value: "md5:abc123"
        }
    }
}
EOF`)
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei validate")
			output, err := testExec.Exec("tomei", "validate", "~/schema-test/bad-checksum.cue")
			Expect(err).To(HaveOccurred())
			Expect(output).To(ContainSubstring("validation failed"))
		})
	})

	Context("tomei apply Rejects Invalid Manifests", func() {
		It("rejects schema-invalid manifest and does not modify state", func() {
			By("Recording state.json before apply attempt")
			stateBefore, err := testExec.ExecBash("cat ~/.local/share/tomei/state.json")
			Expect(err).NotTo(HaveOccurred())

			By("Writing manifest with HTTP URL")
			_, err = testExec.ExecBash(`cat > ~/schema-test/apply-invalid.cue << 'EOF'
apiVersion: "tomei.terassyi.net/v1beta1"
kind:       "Tool"
metadata: name: "test"
spec: {
    installerRef: "download"
    version:      "1.0.0"
    source: {
        url: "http://example.com/tool.tar.gz"
    }
}
EOF`)
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei apply - should fail")
			output, err := testExec.Exec("tomei", "apply", "--yes", "~/schema-test/apply-invalid.cue")
			Expect(err).To(HaveOccurred())
			Expect(output).To(ContainSubstring("failed to load resources"))

			By("Verifying state.json was not modified")
			stateAfter, err := testExec.ExecBash("cat ~/.local/share/tomei/state.json")
			Expect(err).NotTo(HaveOccurred())
			Expect(stateAfter).To(Equal(stateBefore))
		})
	})

	Context("Error Message Quality", func() {
		It("includes resource name in schema validation error", func() {
			By("Creating directory with named resource that has invalid URL")
			_, err := testExec.ExecBash("mkdir -p ~/schema-test/bad-dir")
			Expect(err).NotTo(HaveOccurred())
			_, err = testExec.ExecBash(`cat > ~/schema-test/bad-dir/bad.cue << 'EOF'
package tomei

badTool: {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind:       "Tool"
    metadata: name: "test"
    spec: {
        installerRef: "download"
        version:      "1.0.0"
        source: {
            url: "http://insecure.example.com/tool.tar.gz"
        }
    }
}
EOF`)
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei validate on directory")
			output, err := testExec.Exec("tomei", "validate", "~/schema-test/bad-dir/")
			Expect(err).To(HaveOccurred())
			Expect(output).To(ContainSubstring("schema validation failed"))
			Expect(output).To(ContainSubstring("badTool"))
		})
	})
}
