//go:build e2e

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func tagTests() {

	BeforeAll(func() {
		_, _ = testExec.Exec("tomei", "init", "--yes")
		_, err := testExec.ExecBash("mkdir -p ~/tag-test/cue.mod")
		Expect(err).NotTo(HaveOccurred())
		_, err = testExec.ExecBash(`cat > ~/tag-test/cue.mod/module.cue << 'EOF'
module: "tomei.local@v0"
language: version: "v0.9.0"
EOF`)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		_, _ = testExec.ExecBash("rm -rf ~/tag-test")
	})

	Context("User-Defined @tag()", func() {
		It("resolves OS and arch in user-defined @tag() manifest", func() {
			By("Writing manifest with @tag(os) and @tag(arch)")
			_, err := testExec.ExecBash(`cat > ~/tag-test/tag-tool.cue << 'EOF'
package tomei

_os:   string @tag(os)
_arch: string @tag(arch)

myTool: {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind:       "Tool"
    metadata: name: "tag-test-tool"
    spec: {
        installerRef: "download"
        version:      "1.0.0"
        source: {
            url: "https://example.com/tool_\(_os)_\(_arch).tar.gz"
        }
    }
}
EOF`)
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei validate on @tag() manifest")
			output, err := testExec.Exec("tomei", "validate", "~/tag-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("tag-test-tool"))
			Expect(output).To(ContainSubstring("Validation successful"))
		})
	})

	Context("@if() File-Level Platform Branching", func() {
		BeforeEach(func() {
			// Clean up any previous @if() test files
			_, _ = testExec.ExecBash("rm -f ~/tag-test/if-*.cue ~/tag-test/common.cue")
		})

		It("includes only the current platform file via @if()", func() {
			By("Writing common manifest")
			_, err := testExec.ExecBash(`cat > ~/tag-test/common.cue << 'EOF'
package tomei

commonTool: {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind:       "Tool"
    metadata: name: "if-common-tool"
    spec: {
        installerRef: "download"
        version:      "1.0.0"
        source: url: "https://example.com/common.tar.gz"
    }
}
EOF`)
			Expect(err).NotTo(HaveOccurred())

			By("Writing @if(darwin) manifest")
			_, err = testExec.ExecBash(`cat > ~/tag-test/if-darwin.cue << 'EOF'
@if(darwin)

package tomei

darwinTool: {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind:       "Tool"
    metadata: name: "if-darwin-tool"
    spec: {
        installerRef: "download"
        version:      "1.0.0"
        source: url: "https://example.com/darwin.tar.gz"
    }
}
EOF`)
			Expect(err).NotTo(HaveOccurred())

			By("Writing @if(linux) manifest")
			_, err = testExec.ExecBash(`cat > ~/tag-test/if-linux.cue << 'EOF'
@if(linux)

package tomei

linuxTool: {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind:       "Tool"
    metadata: name: "if-linux-tool"
    spec: {
        installerRef: "download"
        version:      "1.0.0"
        source: url: "https://example.com/linux.tar.gz"
    }
}
EOF`)
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei validate — only current platform resources should load")
			output, err := testExec.Exec("tomei", "validate", "~/tag-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("if-common-tool"))
			Expect(output).To(ContainSubstring("Validation successful"))

			// E2E container is linux — linux-tool should be present, darwin-tool should not
			Expect(output).To(ContainSubstring("if-linux-tool"))
			Expect(output).NotTo(ContainSubstring("if-darwin-tool"))
		})
	})

	// NOTE: Preset import tests (import "tomei.terassyi.net/presets/go") require the
	// module to be published to the OCI registry (ghcr.io/terassyi). These are
	// covered by modregistrytest-based integration tests in
	// tests/cue_ecosystem_integration_test.go instead.
}
