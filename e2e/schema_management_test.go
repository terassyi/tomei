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

	// NOTE: Schema import tests (import "tomei.terassyi.net/schema") require the
	// module to be published to the OCI registry (ghcr.io/terassyi). These are
	// covered by modregistrytest-based integration tests in
	// tests/cue_ecosystem_integration_test.go instead.

	Context("Schema Validation Without Import", func() {
		It("validates manifest without schema import via internal schema", func() {
			dir := "~/schema-mgmt-test/no-import"

			By("Setting up cue.mod via tomei cue init")
			_, err := testExec.ExecBash("mkdir -p " + dir)
			Expect(err).NotTo(HaveOccurred())
			_, err = testExec.ExecBash("tomei cue init " + dir)
			Expect(err).NotTo(HaveOccurred())

			By("Writing manifest without schema import")
			_, err = testExec.ExecBash(`cat > ` + dir + `/tools.cue << 'EOF'
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

			By("Running tomei validate — should succeed via internal schema")
			output, err := testExec.Exec("tomei", "validate", dir+"/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Validation successful"))
		})

		It("rejects invalid resource via internal schema", func() {
			dir := "~/schema-mgmt-test/invalid-no-import"

			By("Setting up cue.mod via tomei cue init")
			_, err := testExec.ExecBash("mkdir -p " + dir)
			Expect(err).NotTo(HaveOccurred())
			_, err = testExec.ExecBash("tomei cue init " + dir)
			Expect(err).NotTo(HaveOccurred())

			By("Writing manifest with HTTP URL (should fail schema validation)")
			_, err = testExec.ExecBash(`cat > ` + dir + `/tools.cue << 'EOF'
package tomei

badTool: {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind:       "Tool"
    metadata: name: "test"
    spec: {
        installerRef: "download"
        version:      "1.0.0"
        source: url: "http://insecure.example.com/test.tar.gz"
    }
}
EOF`)
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei validate — should fail")
			output, err := testExec.Exec("tomei", "validate", dir+"/")
			Expect(err).To(HaveOccurred())
			Expect(output).To(ContainSubstring("schema validation failed"))
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
