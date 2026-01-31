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

var _ = Describe("toto on Ubuntu", Ordered, func() {
	// === Basic Commands ===
	It("displays version information", func() {
		By("Running toto version command")
		output, err := containerExec("toto", "version")
		Expect(err).NotTo(HaveOccurred())

		By("Checking output contains version string")
		Expect(output).To(ContainSubstring("toto version"))
	})

	It("initializes environment with toto init", func() {
		By("Running toto init --yes to create config.cue and directories")
		output, err := containerExec("toto", "init", "--yes")
		Expect(err).NotTo(HaveOccurred())
		Expect(output).To(ContainSubstring("Initialization complete"))

		By("Verifying config.cue was created")
		output, err = containerExecBash("cat ~/.config/toto/config.cue")
		Expect(err).NotTo(HaveOccurred())
		Expect(output).To(ContainSubstring("package toto"))

		By("Verifying data directory was created")
		_, err = containerExecBash("ls -d ~/.local/share/toto")
		Expect(err).NotTo(HaveOccurred())

		By("Verifying bin directory was created")
		_, err = containerExecBash("ls -d ~/.local/bin")
		Expect(err).NotTo(HaveOccurred())

		By("Verifying state.json was created")
		output, err = containerExecBash("cat ~/.local/share/toto/state.json")
		Expect(err).NotTo(HaveOccurred())
		Expect(output).To(ContainSubstring(`"version"`))
	})

	It("validates CUE configuration", func() {
		By("Running toto validate command")
		output, err := containerExec("toto", "validate")
		Expect(err).NotTo(HaveOccurred())

		By("Checking validation succeeded")
		Expect(output).To(ContainSubstring("Validation successful"))

		By("Checking Tool/gh is recognized")
		Expect(output).To(ContainSubstring("Tool/gh"))

		By("Checking Runtime/go is recognized")
		Expect(output).To(ContainSubstring("Runtime/go"))
	})

	It("shows planned changes", func() {
		By("Running toto plan command")
		output, err := containerExec("toto", "plan")
		Expect(err).NotTo(HaveOccurred())

		By("Checking plan shows resources")
		Expect(output).To(ContainSubstring("Found"))
		Expect(output).To(ContainSubstring("resource"))
	})

	// === Apply: Install Runtime and Tool ===
	It("downloads and installs Runtime and Tool", func() {
		By("Running toto apply command")
		output, err := containerExec("toto", "apply")
		Expect(err).NotTo(HaveOccurred())

		By("Checking runtime installation")
		Expect(output).To(ContainSubstring("installing runtime"))
		Expect(output).To(ContainSubstring("name=go"))
		Expect(output).To(ContainSubstring("runtime installed"))

		By("Checking tool installation")
		Expect(output).To(ContainSubstring("installing tool"))
		Expect(output).To(ContainSubstring("name=gh"))
		Expect(output).To(ContainSubstring("tool installed"))
	})

	// === Verify Runtime Installation ===
	It("places runtime in runtimes directory", func() {
		By("Listing runtimes directory")
		output, err := containerExecBash("ls ~/.local/share/toto/runtimes/go/1.25.5/")
		Expect(err).NotTo(HaveOccurred())

		By("Checking bin directory exists")
		Expect(output).To(ContainSubstring("bin"))
	})

	It("creates symlinks for runtime binaries", func() {
		By("Listing bin directory")
		output, err := containerExecBash("ls -la ~/.local/bin/")
		Expect(err).NotTo(HaveOccurred())

		By("Checking symlink to go exists")
		Expect(output).To(ContainSubstring("go ->"))

		By("Checking symlink to gofmt exists")
		Expect(output).To(ContainSubstring("gofmt ->"))
	})

	It("allows running go command after install", func() {
		By("Executing go version")
		output, err := containerExec("go", "version")
		Expect(err).NotTo(HaveOccurred())

		By("Checking go version output")
		Expect(output).To(ContainSubstring("go1.25.5"))
	})

	It("allows running gofmt command after install", func() {
		By("Executing gofmt -h to verify it works")
		output, err := containerExecBash("gofmt -h 2>&1 || true")
		Expect(err).NotTo(HaveOccurred())

		By("Checking gofmt output")
		Expect(output).To(ContainSubstring("usage"))
	})

	// === Verify Tool Installation ===
	It("places tool binary in tools directory", func() {
		By("Listing tools directory")
		output, err := containerExecBash("ls ~/.local/share/toto/tools/gh/2.86.0/")
		Expect(err).NotTo(HaveOccurred())

		By("Checking gh binary exists")
		Expect(output).To(ContainSubstring("gh"))
	})

	It("creates symlink for tool in bin directory", func() {
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

	// === Verify State ===
	It("updates state.json with runtime and tool info", func() {
		By("Reading state.json")
		output, err := containerExecBash("cat ~/.local/share/toto/state.json")
		Expect(err).NotTo(HaveOccurred())

		By("Checking runtimes section exists")
		Expect(output).To(ContainSubstring(`"runtimes"`))

		By("Checking go runtime version is recorded")
		Expect(output).To(ContainSubstring(`"version": "1.25.5"`))

		By("Checking tools section exists")
		Expect(output).To(ContainSubstring(`"tools"`))

		By("Checking gh tool version is recorded")
		Expect(output).To(ContainSubstring(`"version": "2.86.0"`))
	})

	// === Idempotency ===
	It("is idempotent on subsequent applies", func() {
		By("Running toto apply again")
		output, err := containerExec("toto", "apply")
		Expect(err).NotTo(HaveOccurred())

		By("Checking no changes to apply")
		Expect(output).To(ContainSubstring("no changes to apply"))
	})

	It("does not re-download on multiple applies", func() {
		By("Running toto apply two more times")
		output1, err := containerExec("toto", "apply")
		Expect(err).NotTo(HaveOccurred())
		output2, err := containerExec("toto", "apply")
		Expect(err).NotTo(HaveOccurred())

		By("Checking no installations occurred")
		Expect(output1).NotTo(ContainSubstring("installed successfully"))
		Expect(output2).NotTo(ContainSubstring("installed successfully"))

		By("Checking go still works")
		output, err := containerExec("go", "version")
		Expect(err).NotTo(HaveOccurred())
		Expect(output).To(ContainSubstring("go1.25.5"))

		By("Checking gh still works")
		output, err = containerExec("gh", "--version")
		Expect(err).NotTo(HaveOccurred())
		Expect(output).To(ContainSubstring("gh version 2.86.0"))
	})
})
