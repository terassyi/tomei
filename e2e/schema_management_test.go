//go:build e2e

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func schemaManagementTests() {

	BeforeAll(func() {
		// Ensure tomei is initialized
		_, _ = testExec.Exec("tomei", "init", "--yes")
	})

	AfterAll(func() {
		_, _ = testExec.ExecBash("rm -rf ~/schema-mgmt-test")
	})

	Context("Schema Import", func() {
		It("validates manifest using import tomei.terassyi.net/schema", func() {
			By("Creating directory with schema-import manifest")
			_, err := testExec.ExecBash("mkdir -p ~/schema-mgmt-test/import-test")
			Expect(err).NotTo(HaveOccurred())

			_, err = testExec.ExecBash(`cat > ~/schema-mgmt-test/import-test/tools.cue << 'EOF'
package tomei

import "tomei.terassyi.net/schema"

myTool: schema.#Tool & {
    metadata: name: "jq"
    spec: {
        installerRef: "aqua"
        version:      "1.7.1"
        package:      "jqlang/jq"
    }
}
EOF`)
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei validate — should succeed")
			output, err := testExec.Exec("tomei", "validate", "~/schema-mgmt-test/import-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Validation successful"))
		})

		It("rejects invalid resource via schema import", func() {
			By("Creating manifest with wrong apiVersion via schema.#Tool")
			_, err := testExec.ExecBash("mkdir -p ~/schema-mgmt-test/import-invalid")
			Expect(err).NotTo(HaveOccurred())

			_, err = testExec.ExecBash(`cat > ~/schema-mgmt-test/import-invalid/tools.cue << 'EOF'
package tomei

import "tomei.terassyi.net/schema"

badTool: schema.#Tool & {
    apiVersion: "wrong/v1"
    metadata: name: "test"
    spec: {
        installerRef: "download"
        version:      "1.0.0"
    }
}
EOF`)
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei validate — should fail")
			_, err = testExec.Exec("tomei", "validate", "~/schema-mgmt-test/import-invalid/")
			Expect(err).To(HaveOccurred())
		})
	})

	Context("Init Guard", func() {
		It("rejects apply before init", func() {
			By("Creating a fresh home directory to simulate uninitialised state")
			_, err := testExec.ExecBash("mkdir -p ~/schema-mgmt-test/fresh-home")
			Expect(err).NotTo(HaveOccurred())

			_, err = testExec.ExecBash(`cat > ~/schema-mgmt-test/fresh-home/tools.cue << 'EOF'
apiVersion: "tomei.terassyi.net/v1beta1"
kind:       "Tool"
metadata: name: "jq"
spec: {
    installerRef: "aqua"
    version:      "1.7.1"
}
EOF`)
			Expect(err).NotTo(HaveOccurred())

			By("Removing state.json to simulate uninitialised state")
			_, _ = testExec.ExecBash("mv ~/.local/share/tomei/state.json ~/.local/share/tomei/state.json.bak")

			By("Running tomei apply — should fail with init message")
			output, err := testExec.Exec("tomei", "apply", "--yes", "~/schema-mgmt-test/fresh-home/tools.cue")
			Expect(err).To(HaveOccurred())
			Expect(output).To(ContainSubstring("tomei is not initialized"))
			Expect(output).To(ContainSubstring("tomei init"))

			By("Restoring state.json")
			_, _ = testExec.ExecBash("mv ~/.local/share/tomei/state.json.bak ~/.local/share/tomei/state.json")
		})
	})

	Context("Apply Confirmation Prompt", func() {
		It("proceeds with --yes flag", func() {
			By("Running tomei apply with --yes on manifests")
			output, err := ExecApply(testExec, "~/manifests/")
			Expect(err).NotTo(HaveOccurred())
			// Should not contain the prompt text
			Expect(output).NotTo(ContainSubstring("[y/N]"))
		})
	})
}
