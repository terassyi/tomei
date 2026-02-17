//go:build e2e

package e2e

import (
	"encoding/json"

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

		It("resolves module version from OCI registry", func() {
			dir := "~/cue-ecosystem-test/init-registry-version"
			_, err := testExec.ExecBash("mkdir -p " + dir)
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei cue init (should query ghcr.io for latest version)")
			output, err := testExec.ExecBash("tomei cue init " + dir)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("module.cue"))

			By("Verifying module.cue contains a version resolved from the registry")
			content, err := testExec.ExecBash("cat " + dir + "/cue.mod/module.cue")
			Expect(err).NotTo(HaveOccurred())
			// The version should be a real semver from the registry, not the hardcoded default.
			// At minimum v0.0.1 is published, but the registry may have newer versions.
			Expect(content).To(MatchRegexp(`v: "v0\.\d+\.\d+"`))
			Expect(content).To(ContainSubstring(`"tomei.terassyi.net@v0"`))
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

	Context("tomei cue scaffold", func() {
		It("outputs a tool scaffold with schema import", func() {
			output, err := testExec.Exec("tomei", "cue", "scaffold", "tool")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring(`import "tomei.terassyi.net/schema"`))
			Expect(output).To(ContainSubstring("schema.#Tool"))
			Expect(output).To(ContainSubstring(`kind:       "Tool"`))
		})

		It("outputs a bare tool scaffold without import", func() {
			output, err := testExec.Exec("tomei", "cue", "scaffold", "tool", "--bare")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).NotTo(ContainSubstring("import"))
			Expect(output).To(ContainSubstring(`kind:       "Tool"`))
		})

		It("supports all resource kinds", func() {
			kinds := []string{"tool", "runtime", "installer", "installer-repository", "toolset"}
			for _, kind := range kinds {
				output, err := testExec.Exec("tomei", "cue", "scaffold", kind)
				Expect(err).NotTo(HaveOccurred(), "scaffold %s failed", kind)
				Expect(output).To(ContainSubstring("package tomei"), "scaffold %s missing package", kind)
			}
		})

		It("rejects unknown kind", func() {
			_, err := testExec.Exec("tomei", "cue", "scaffold", "unknown")
			Expect(err).To(HaveOccurred())
		})
	})

	Context("tomei cue eval", func() {
		var evalDir string

		BeforeAll(func() {
			evalDir = "~/cue-ecosystem-test/eval-test"
			_, err := testExec.ExecBash("mkdir -p " + evalDir)
			Expect(err).NotTo(HaveOccurred())
			_, err = testExec.ExecBash("tomei cue init " + evalDir)
			Expect(err).NotTo(HaveOccurred())

			_, err = testExec.ExecBash(`cat > ` + evalDir + `/tools.cue << 'EOF'
package tomei

myTool: {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind:       "Tool"
    metadata: name: "jq"
    spec: {
        installerRef: "aqua"
        version:      "1.7.1"
        package:      "jqlang/jq"
    }
}
EOF`)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterAll(func() {
			_, _ = testExec.ExecBash("rm -rf " + evalDir)
		})

		It("outputs CUE text with resolved values", func() {
			output, err := testExec.ExecBash("tomei cue eval " + evalDir + "/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring(`"tomei.terassyi.net/v1beta1"`))
			Expect(output).To(ContainSubstring(`"jq"`))
			Expect(output).To(ContainSubstring(`"aqua"`))
		})

		It("resolves @tag() values", func() {
			_, err := testExec.ExecBash(`cat > ` + evalDir + `/runtime.cue << 'EOF'
package tomei

_os:   string @tag(os)
_arch: string @tag(arch)

testRuntime: {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind:       "Runtime"
    metadata: name: "test-rt"
    spec: {
        type:    "download"
        version: "1.0.0"
        toolBinPath: "~/test/bin"
        source: url: "https://example.com/rt_\(_os)_\(_arch).tar.gz"
    }
}
EOF`)
			Expect(err).NotTo(HaveOccurred())

			output, err := testExec.ExecBash("tomei cue eval " + evalDir + "/")
			Expect(err).NotTo(HaveOccurred())
			// @tag(os) and @tag(arch) should be resolved to actual values
			Expect(output).To(MatchRegexp(`https://example\.com/rt_(linux|darwin)_(amd64|arm64)\.tar\.gz`))

			// cleanup runtime.cue to not affect other tests
			_, _ = testExec.ExecBash("rm -f " + evalDir + "/runtime.cue")
		})
	})

	Context("tomei cue export", func() {
		var exportDir string

		BeforeAll(func() {
			exportDir = "~/cue-ecosystem-test/export-test"
			_, err := testExec.ExecBash("mkdir -p " + exportDir)
			Expect(err).NotTo(HaveOccurred())
			_, err = testExec.ExecBash("tomei cue init " + exportDir)
			Expect(err).NotTo(HaveOccurred())

			_, err = testExec.ExecBash(`cat > ` + exportDir + `/tools.cue << 'EOF'
package tomei

myTool: {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind:       "Tool"
    metadata: name: "jq"
    spec: {
        installerRef: "aqua"
        version:      "1.7.1"
        package:      "jqlang/jq"
    }
}
EOF`)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterAll(func() {
			_, _ = testExec.ExecBash("rm -rf " + exportDir)
		})

		It("outputs valid JSON", func() {
			output, err := testExec.ExecBash("tomei cue export " + exportDir + "/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring(`"apiVersion"`))
			Expect(output).To(ContainSubstring(`"tomei.terassyi.net/v1beta1"`))
			Expect(output).To(ContainSubstring(`"jq"`))

			// JSON should be parseable
			var parsed map[string]interface{}
			Expect(json.Unmarshal([]byte(output), &parsed)).To(Succeed())
		})
	})
}
