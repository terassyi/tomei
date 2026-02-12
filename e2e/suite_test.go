//go:build e2e

package e2e

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	// Enable verbose output to capture stdout from tests
	RunSpecs(t, "E2E Suite", Label("e2e"))
}

var _ = BeforeSuite(func() {
	var err error
	testExec, err = newExecutor()
	if err != nil {
		Skip(err.Error())
	}
	Expect(testExec.Setup()).To(Succeed())

	versions, err = loadVersions()
	Expect(err).NotTo(HaveOccurred(), "failed to load versions from CUE manifests")
})

var _ = AfterSuite(func() {
	if testExec != nil {
		testExec.Cleanup()
	}
})

// Single top-level Describe with Ordered to guarantee execution order across all contexts.
var _ = Describe("tomei E2E", Ordered, func() {
	Context("Basic", basicTests)
	Context("Schema Validation", schemaValidationTests)
	Context("Schema Management", schemaManagementTests)
	Context("State Backup and Diff", stateBackupDiffTests)
	Context("ToolSet", toolsetTests)
	Context("Aqua Registry", registryTests)
	Context("Delegation Runtime", delegationTests)
	Context("Installer Repository", installerRepositoryTests)
	Context("Dependency Resolution", dependencyTests)
	Context("Installation Logs", logsTests)
	Context("Get Command", getTests)
	Context("Completion", completionTests)
	Context("CUE Tags", tagTests)
})
