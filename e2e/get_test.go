//go:build e2e

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func getTests() {

	BeforeAll(func() {
		By("Ensuring tomei is initialized and resources are applied")
		_, _ = testExec.Exec("tomei", "init", "--yes")
		// Reset state to avoid interference from previous test contexts
		_, _ = testExec.ExecBash(`echo '{"runtimes":{},"tools":{},"installers":{},"installerRepositories":{}}' > ~/.local/share/tomei/state.json`)
		_, err := ExecApply(testExec, "~/manifests/")
		Expect(err).NotTo(HaveOccurred())
	})

	Context("Table Output", func() {
		It("lists tools in table format", func() {
			output, err := testExec.Exec("tomei", "get", "tools")
			Expect(err).NotTo(HaveOccurred())

			By("Checking header columns")
			Expect(output).To(ContainSubstring("NAME"))
			Expect(output).To(ContainSubstring("VERSION"))
			Expect(output).To(ContainSubstring("VERSION_KIND"))

			By("Checking installed tools appear")
			Expect(output).To(ContainSubstring("gh"))
			Expect(output).To(ContainSubstring(versions.GhVersion))
		})

		It("lists runtimes in table format", func() {
			output, err := testExec.Exec("tomei", "get", "runtimes")
			Expect(err).NotTo(HaveOccurred())

			By("Checking header columns")
			Expect(output).To(ContainSubstring("NAME"))
			Expect(output).To(ContainSubstring("TYPE"))

			By("Checking installed runtimes appear")
			Expect(output).To(ContainSubstring("go"))
			Expect(output).To(ContainSubstring(versions.GoVersion))
		})

		It("filters by resource name", func() {
			output, err := testExec.Exec("tomei", "get", "tools", "gh")
			Expect(err).NotTo(HaveOccurred())

			By("Checking only gh appears")
			Expect(output).To(ContainSubstring("gh"))
			Expect(output).NotTo(ContainSubstring("gopls"))
		})

		It("shows 'No resources found.' for nonexistent name", func() {
			output, err := testExec.Exec("tomei", "get", "tools", "nonexistent")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("No resources found."))
		})
	})

	Context("Wide Output", func() {
		It("shows additional columns for tools", func() {
			output, err := testExec.Exec("tomei", "get", "tools", "-o", "wide")
			Expect(err).NotTo(HaveOccurred())

			By("Checking wide columns")
			Expect(output).To(ContainSubstring("PACKAGE"))
			Expect(output).To(ContainSubstring("BIN_PATH"))
		})

		It("shows additional columns for runtimes", func() {
			output, err := testExec.Exec("tomei", "get", "runtimes", "-o", "wide")
			Expect(err).NotTo(HaveOccurred())

			By("Checking wide columns")
			Expect(output).To(ContainSubstring("INSTALL_PATH"))
			Expect(output).To(ContainSubstring("BINARIES"))
		})
	})

	Context("JSON Output", func() {
		It("outputs tools as JSON", func() {
			output, err := testExec.Exec("tomei", "get", "tools", "-o", "json")
			Expect(err).NotTo(HaveOccurred())

			By("Checking JSON structure")
			Expect(output).To(ContainSubstring(`"gh"`))
			Expect(output).To(ContainSubstring(fmt.Sprintf(`"version": "%s"`, versions.GhVersion)))
		})

		It("outputs runtimes as JSON", func() {
			output, err := testExec.Exec("tomei", "get", "runtimes", "-o", "json")
			Expect(err).NotTo(HaveOccurred())

			By("Checking JSON structure")
			Expect(output).To(ContainSubstring(`"go"`))
			Expect(output).To(ContainSubstring(fmt.Sprintf(`"version": "%s"`, versions.GoVersion)))
		})

		It("filters JSON output by name", func() {
			output, err := testExec.Exec("tomei", "get", "tools", "gh", "-o", "json")
			Expect(err).NotTo(HaveOccurred())

			Expect(output).To(ContainSubstring(`"gh"`))
			Expect(output).NotTo(ContainSubstring(`"gopls"`))
		})
	})

	Context("Resource Type Aliases", func() {
		It("accepts 'tool' alias", func() {
			output, err := testExec.Exec("tomei", "get", "tool")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("NAME"))
		})

		It("accepts 'rt' alias for runtimes", func() {
			output, err := testExec.Exec("tomei", "get", "rt")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("NAME"))
		})
	})

	Context("Error Handling", func() {
		It("returns error for unknown resource type", func() {
			output, err := testExec.Exec("tomei", "get", "unknown")
			Expect(err).To(HaveOccurred())

			By("Checking error message mentions unknown resource type")
			Expect(output).To(ContainSubstring("unknown resource type"))
		})
	})

	Context("Empty Resources", func() {
		It("shows 'No resources found.' for empty installers", func() {
			output, err := testExec.Exec("tomei", "get", "installers")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("No resources found."))
		})
	})
}
