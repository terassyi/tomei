//go:build e2e

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func tagTests() {

	BeforeAll(func() {
		_, _ = testExec.Exec("tomei", "init", "--yes")
		_, err := testExec.ExecBash("mkdir -p ~/tag-test")
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

	Context("Preset Import", func() {
		It("resolves platform via explicit parameter", func() {
			By("Writing manifest that imports Go preset with platform parameter")
			_, err := testExec.ExecBash(`cat > ~/tag-test/preset-import.cue << 'EOF'
package tomei

import gopreset "tomei.terassyi.net/presets/go"

_os:   string @tag(os)
_arch: string @tag(arch)

goRuntime: gopreset.#GoRuntime & {
    platform: { os: _os, arch: _arch }
    spec: version: "1.25.6"
}
EOF`)
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei validate on preset import manifest")
			output, err := testExec.Exec("tomei", "validate", "~/tag-test/preset-import.cue")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Runtime/go"))
			Expect(output).To(ContainSubstring("Validation successful"))
		})
	})
}
