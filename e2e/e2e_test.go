package e2e_test

import (
	"os"
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var containerName string

var _ = BeforeSuite(func() {
	containerName = os.Getenv("TOTO_E2E_CONTAINER")
	Expect(containerName).NotTo(BeEmpty(), "TOTO_E2E_CONTAINER environment variable must be set")
})

func containerExec(args ...string) (string, error) {
	cmdArgs := append([]string{"exec", containerName}, args...)
	cmd := exec.Command("docker", cmdArgs...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func containerExecBash(script string) (string, error) {
	return containerExec("bash", "-c", script)
}

var _ = Describe("toto on Ubuntu", func() {
	It("displays version information", func() {
		By("Running toto version command")
		output, err := containerExec("toto", "version")
		Expect(err).NotTo(HaveOccurred())

		By("Checking output contains version string")
		Expect(output).To(ContainSubstring("toto version"))
	})

	It("validates CUE configuration", func() {
		By("Running toto validate command")
		output, err := containerExec("toto", "validate")
		Expect(err).NotTo(HaveOccurred())

		By("Checking validation succeeded")
		Expect(output).To(ContainSubstring("Validation successful"))

		By("Checking Tool/gh is recognized")
		Expect(output).To(ContainSubstring("Tool/gh"))
	})

	It("shows planned changes", func() {
		By("Running toto plan command")
		output, err := containerExec("toto", "plan")
		Expect(err).NotTo(HaveOccurred())

		By("Checking plan shows 1 resource")
		Expect(output).To(ContainSubstring("Found 1 resource"))
	})

	It("downloads and installs gh CLI from GitHub", func() {
		By("Running toto apply command")
		output, err := containerExec("toto", "apply")
		Expect(err).NotTo(HaveOccurred())

		By("Checking installation started")
		Expect(output).To(ContainSubstring("installing tool"))

		By("Checking gh tool is being installed")
		Expect(output).To(ContainSubstring("name=gh"))

		By("Checking installation completed")
		Expect(output).To(ContainSubstring("tool installed"))
	})

	It("places binary in tools directory", func() {
		By("Listing tools directory")
		output, err := containerExecBash("ls ~/.local/share/toto/tools/gh/2.86.0/")
		Expect(err).NotTo(HaveOccurred())

		By("Checking gh binary exists")
		Expect(output).To(ContainSubstring("gh"))
	})

	It("creates symlink in bin directory", func() {
		By("Listing bin directory")
		output, err := containerExecBash("ls -la ~/.local/bin/")
		Expect(err).NotTo(HaveOccurred())

		By("Checking symlink to gh exists")
		Expect(output).To(ContainSubstring("gh ->"))
	})

	It("allows running gh command after install", func() {
		By("Executing gh --version")
		output, err := containerExec("gh", "--version")
		Expect(err).NotTo(HaveOccurred())

		By("Checking gh version output")
		Expect(output).To(ContainSubstring("gh version 2.86.0"))
	})

	It("updates state.json after install", func() {
		By("Reading state.json")
		output, err := containerExecBash("cat ~/.local/share/toto/state.json")
		Expect(err).NotTo(HaveOccurred())

		By("Checking version is recorded in state")
		Expect(output).To(ContainSubstring(`"version": "2.86.0"`))

		By("Checking installerRef is recorded in state")
		Expect(output).To(ContainSubstring(`"installerRef": "download"`))
	})

	It("is idempotent on subsequent applies", func() {
		By("Running toto apply again")
		output, err := containerExec("toto", "apply")
		Expect(err).NotTo(HaveOccurred())

		By("Checking no changes to apply")
		Expect(output).To(ContainSubstring("no changes to apply"))
	})

	It("does not re-download binary on multiple applies", func() {
		By("Running toto apply two more times")
		output1, err := containerExec("toto", "apply")
		Expect(err).NotTo(HaveOccurred())
		output2, err := containerExec("toto", "apply")
		Expect(err).NotTo(HaveOccurred())

		By("Checking no installations occurred")
		Expect(output1).NotTo(ContainSubstring("tool installed successfully"))
		Expect(output2).NotTo(ContainSubstring("tool installed successfully"))

		By("Checking gh still works")
		output, err := containerExec("gh", "--version")
		Expect(err).NotTo(HaveOccurred())
		Expect(output).To(ContainSubstring("gh version 2.86.0"))
	})
})
