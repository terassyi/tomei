//go:build e2e

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func cueEcosystemTests() {

	BeforeAll(func() {
		_, _ = testExec.Exec("tomei", "init", "--yes")
		_, err := testExec.ExecBash("mkdir -p ~/cue-ecosystem-test")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		_, _ = testExec.ExecBash("rm -rf ~/cue-ecosystem-test")
	})

	// NOTE: tomei cue init does not expand ~ (unlike LoadPaths), so all
	// cue init invocations use ExecBash for shell tilde expansion.

	Context("tomei cue init", func() {
		It("creates cue.mod/module.cue and tomei_platform.cue", func() {
			dir := "~/cue-ecosystem-test/init-basic"
			_, err := testExec.ExecBash("mkdir -p " + dir)
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei cue init")
			output, err := testExec.ExecBash("tomei cue init " + dir)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("module.cue"))
			Expect(output).To(ContainSubstring("tomei_platform.cue"))

			By("Verifying cue.mod/module.cue exists and contains expected content")
			content, err := testExec.ExecBash("cat " + dir + "/cue.mod/module.cue")
			Expect(err).NotTo(HaveOccurred())
			Expect(content).To(ContainSubstring(`module: "manifests.local@v0"`))
			Expect(content).To(ContainSubstring(`language: version:`))
			Expect(content).To(ContainSubstring(`"tomei.terassyi.net@v0"`))

			By("Verifying tomei_platform.cue exists and contains @tag declarations")
			platform, err := testExec.ExecBash("cat " + dir + "/tomei_platform.cue")
			Expect(err).NotTo(HaveOccurred())
			Expect(platform).To(ContainSubstring("package tomei"))
			Expect(platform).To(ContainSubstring("@tag(os)"))
			Expect(platform).To(ContainSubstring("@tag(arch)"))
			Expect(platform).To(ContainSubstring("@tag(headless,type=bool)"))
		})

		It("refuses overwrite without --force", func() {
			dir := "~/cue-ecosystem-test/init-noforce"
			_, err := testExec.ExecBash("mkdir -p " + dir)
			Expect(err).NotTo(HaveOccurred())

			By("Creating files first time")
			_, err = testExec.ExecBash("tomei cue init " + dir)
			Expect(err).NotTo(HaveOccurred())

			By("Running cue init again without --force")
			_, err = testExec.ExecBash("tomei cue init " + dir)
			Expect(err).To(HaveOccurred())
		})

		It("overwrites with --force", func() {
			dir := "~/cue-ecosystem-test/init-force"
			_, err := testExec.ExecBash("mkdir -p " + dir)
			Expect(err).NotTo(HaveOccurred())

			By("Creating files first time")
			_, err = testExec.ExecBash("tomei cue init " + dir)
			Expect(err).NotTo(HaveOccurred())

			By("Running cue init again with --force")
			output, err := testExec.ExecBash("tomei cue init --force " + dir)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("module.cue"))
		})

		It("accepts --module-name", func() {
			dir := "~/cue-ecosystem-test/init-module-name"
			_, err := testExec.ExecBash("mkdir -p " + dir)
			Expect(err).NotTo(HaveOccurred())

			By("Running cue init with custom module name")
			_, err = testExec.ExecBash("tomei cue init --module-name myproject@v0 " + dir)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying custom module name in module.cue")
			content, err := testExec.ExecBash("cat " + dir + "/cue.mod/module.cue")
			Expect(err).NotTo(HaveOccurred())
			Expect(content).To(ContainSubstring(`module: "myproject@v0"`))
		})
	})

	Context("tomei env with cue.mod", func() {
		It("includes CUE_REGISTRY when cue.mod exists", func() {
			dir := "~/cue-ecosystem-test/env-with-cuemod"

			By("Creating a directory with cue.mod")
			_, err := testExec.ExecBash("mkdir -p " + dir)
			Expect(err).NotTo(HaveOccurred())
			_, err = testExec.ExecBash("tomei cue init " + dir)
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei env from directory with cue.mod")
			output, err := testExec.ExecBash("cd " + dir + " && tomei env")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("CUE_REGISTRY"))
		})

		It("omits CUE_REGISTRY when no cue.mod", func() {
			dir := "~/cue-ecosystem-test/env-no-cuemod"

			By("Creating a directory without cue.mod")
			_, err := testExec.ExecBash("mkdir -p " + dir)
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei env from directory without cue.mod")
			output, err := testExec.ExecBash("cd " + dir + " && tomei env")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).NotTo(ContainSubstring("CUE_REGISTRY"))
		})
	})

	Context("validate with cue.mod", func() {
		It("validates manifest with platform tags and no imports", func() {
			dir := "~/cue-ecosystem-test/validate-tags"

			By("Setting up cue.mod via tomei cue init")
			_, err := testExec.ExecBash("mkdir -p " + dir)
			Expect(err).NotTo(HaveOccurred())
			_, err = testExec.ExecBash("tomei cue init " + dir)
			Expect(err).NotTo(HaveOccurred())

			By("Writing a manifest using @tag(os) and @tag(arch)")
			_, err = testExec.ExecBash(`cat > ` + dir + `/tools.cue << 'EOF'
package tomei

_os:   string @tag(os)
_arch: string @tag(arch)

myTool: {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind:       "Tool"
    metadata: name: "cue-tag-test"
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

			By("Running tomei validate")
			output, err := testExec.Exec("tomei", "validate", dir)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("cue-tag-test"))
			Expect(output).To(ContainSubstring("Validation successful"))
		})
	})

	Context("validate without cue.mod", func() {
		It("fails with clear error when importing without cue.mod", func() {
			dir := "~/cue-ecosystem-test/no-cuemod-import"

			By("Creating directory without cue.mod")
			_, err := testExec.ExecBash("mkdir -p " + dir)
			Expect(err).NotTo(HaveOccurred())

			By("Writing manifest that uses import")
			_, err = testExec.ExecBash(`cat > ` + dir + `/tools.cue << 'EOF'
package tomei

import "tomei.terassyi.net/schema"

myTool: schema.#Tool & {
    metadata: name: "test"
    spec: {
        installerRef: "download"
        version:      "1.0.0"
        source: url: "https://example.com/test.tar.gz"
    }
}
EOF`)
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei validate — should fail with cue.mod error")
			output, err := testExec.Exec("tomei", "validate", dir)
			Expect(err).To(HaveOccurred())
			Expect(output).To(ContainSubstring("cue.mod"))
		})
	})
}
