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

	Context("tomei schema Command", func() {
		It("creates schema.cue in a new directory", func() {
			By("Creating target directory")
			_, err := testExec.ExecBash("mkdir -p ~/schema-mgmt-test/new-dir")
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei schema")
			output, err := testExec.Exec("tomei", "schema", "~/schema-mgmt-test/new-dir")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Created"))

			By("Verifying schema.cue content")
			content, err := testExec.ExecBash("cat ~/schema-mgmt-test/new-dir/schema.cue")
			Expect(err).NotTo(HaveOccurred())
			Expect(content).To(ContainSubstring("package tomei"))
			Expect(content).To(ContainSubstring("#APIVersion"))
		})

		It("reports up-to-date when schema.cue matches", func() {
			By("Running tomei schema again on same directory")
			output, err := testExec.Exec("tomei", "schema", "~/schema-mgmt-test/new-dir")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("up to date"))
		})

		It("updates schema.cue when content differs", func() {
			By("Writing outdated schema.cue")
			_, err := testExec.ExecBash(`echo 'package tomei' > ~/schema-mgmt-test/new-dir/schema.cue`)
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei schema")
			output, err := testExec.Exec("tomei", "schema", "~/schema-mgmt-test/new-dir")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Updated"))
		})

		It("defaults to current directory when no argument given", func() {
			By("Running tomei schema without arguments")
			output, err := testExec.Exec("tomei", "schema")
			Expect(err).NotTo(HaveOccurred())
			// schema.cue already exists from init, so it should be up-to-date or updated
			Expect(output).To(Or(ContainSubstring("up to date"), ContainSubstring("Updated"), ContainSubstring("Created")))
		})
	})

	Context("Schema apiVersion Mismatch", func() {
		It("rejects apply when schema.cue has wrong apiVersion", func() {
			By("Creating directory with mismatched schema.cue")
			_, err := testExec.ExecBash("mkdir -p ~/schema-mgmt-test/mismatch")
			Expect(err).NotTo(HaveOccurred())

			_, err = testExec.ExecBash(`cat > ~/schema-mgmt-test/mismatch/schema.cue << 'EOF'
package tomei

#APIVersion: "tomei.terassyi.net/v0old"
EOF`)
			Expect(err).NotTo(HaveOccurred())

			By("Writing a standalone manifest in the same directory")
			_, err = testExec.ExecBash(`cat > ~/schema-mgmt-test/mismatch/tools.cue << 'EOF'
apiVersion: "tomei.terassyi.net/v1beta1"
kind:       "Tool"
metadata: name: "jq"
spec: {
    installerRef: "aqua"
    version:      "1.7.1"
}
EOF`)
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei apply on the manifest file — should fail with apiVersion mismatch")
			output, err := testExec.Exec("tomei", "apply", "--yes", "~/schema-mgmt-test/mismatch/tools.cue")
			Expect(err).To(HaveOccurred())
			Expect(output).To(ContainSubstring("apiVersion mismatch"))
			Expect(output).To(ContainSubstring("tomei schema"))
		})

		It("rejects validate when schema.cue has wrong apiVersion", func() {
			By("Running tomei validate — should fail with apiVersion mismatch")
			output, err := testExec.Exec("tomei", "validate", "~/schema-mgmt-test/mismatch/tools.cue")
			Expect(err).To(HaveOccurred())
			Expect(output).To(ContainSubstring("apiVersion mismatch"))
		})

		It("rejects plan when schema.cue has wrong apiVersion", func() {
			By("Running tomei plan on the manifest file — should fail with apiVersion mismatch")
			output, err := testExec.Exec("tomei", "plan", "~/schema-mgmt-test/mismatch/tools.cue")
			Expect(err).To(HaveOccurred())
			Expect(output).To(ContainSubstring("apiVersion mismatch"))
		})

		It("succeeds when schema.cue has correct apiVersion", func() {
			By("Updating schema.cue to correct version")
			_, err := testExec.Exec("tomei", "schema", "~/schema-mgmt-test/mismatch")
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei validate — should succeed")
			_, err = testExec.Exec("tomei", "validate", "~/schema-mgmt-test/mismatch/tools.cue")
			Expect(err).NotTo(HaveOccurred())
		})

		It("skips check when no schema.cue in directory", func() {
			By("Creating directory without schema.cue")
			_, err := testExec.ExecBash("mkdir -p ~/schema-mgmt-test/no-schema")
			Expect(err).NotTo(HaveOccurred())

			_, err = testExec.ExecBash(`cat > ~/schema-mgmt-test/no-schema/tools.cue << 'EOF'
apiVersion: "tomei.terassyi.net/v1beta1"
kind:       "Tool"
metadata: name: "jq"
spec: {
    installerRef: "aqua"
    version:      "1.7.1"
}
EOF`)
			Expect(err).NotTo(HaveOccurred())

			By("Running tomei validate — should succeed without schema.cue")
			_, err = testExec.Exec("tomei", "validate", "~/schema-mgmt-test/no-schema/tools.cue")
			Expect(err).NotTo(HaveOccurred())
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
