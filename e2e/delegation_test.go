//go:build e2e

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func delegationTests() {

	BeforeAll(func() {
		// Initialize toto (may already be initialized by other tests, ignore errors)
		_, _ = testExec.Exec("toto", "init", "--yes")
		// Reset state to avoid interference from previous test contexts
		_, _ = testExec.ExecBash(`echo '{"runtimes":{},"tools":{},"installers":{}}' > ~/.local/share/toto/state.json`)
	})

	Context("Rust Runtime Installation (Delegation Pattern)", func() {
		It("validates Rust runtime manifest", func() {
			By("Running toto validate on delegation-test directory")
			output, err := testExec.Exec("toto", "validate", "~/delegation-test/")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Validation successful"))
			Expect(output).To(ContainSubstring("Runtime/rust"))
			Expect(output).To(ContainSubstring("Tool/sd"))
		})

		It("installs Rust runtime and tool via delegation", func() {
			By("Running toto apply on delegation-test directory")
			_, err := testExec.Exec("toto", "apply", "~/delegation-test/")
			Expect(err).NotTo(HaveOccurred())
		})

		It("places rustc binary in binDir (~/.cargo/bin)", func() {
			By("Checking rustc exists in cargo bin")
			output, err := testExec.ExecBash("ls ~/.cargo/bin/rustc")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("rustc"))
		})

		It("allows running rustc command after install", func() {
			By("Executing rustc --version")
			output, err := testExec.ExecBash("~/.cargo/bin/rustc --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("rustc"))
		})

		It("allows running cargo command after install", func() {
			By("Executing cargo --version")
			output, err := testExec.ExecBash("~/.cargo/bin/cargo --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("cargo"))
		})

		It("records delegation state in state.json", func() {
			By("Reading state.json")
			output, err := testExec.ExecBash("cat ~/.local/share/toto/state.json")
			Expect(err).NotTo(HaveOccurred())

			By("Checking rust runtime is recorded")
			Expect(output).To(ContainSubstring(`"rust"`))

			By("Checking type is delegation")
			Expect(output).To(ContainSubstring(`"type": "delegation"`))

			By("Checking specVersion is stable")
			Expect(output).To(ContainSubstring(fmt.Sprintf(`"specVersion": "%s"`, versions.RustVersion)))

			By("Checking removeCommand is recorded")
			Expect(output).To(ContainSubstring(`"removeCommand"`))
			Expect(output).To(ContainSubstring(`rustup self uninstall`))

			By("Checking env is recorded")
			Expect(output).To(ContainSubstring(`"CARGO_HOME"`))
			Expect(output).To(ContainSubstring(`"RUSTUP_HOME"`))
		})

		It("exports Rust environment variables", func() {
			By("Running toto env")
			output, err := testExec.Exec("toto", "env")
			Expect(err).NotTo(HaveOccurred())

			By("Checking CARGO_HOME export")
			Expect(output).To(ContainSubstring(`CARGO_HOME`))

			By("Checking PATH includes cargo/bin")
			Expect(output).To(ContainSubstring(`.cargo/bin`))
		})
	})

	Context("Cargo Install Tool (Runtime Delegation)", func() {
		It("places sd binary in toolBinPath (~/.cargo/bin)", func() {
			By("Checking sd binary exists in cargo bin")
			output, err := testExec.ExecBash("ls ~/.cargo/bin/sd")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("sd"))
		})

		It("allows running sd command after install", func() {
			By("Executing sd --version")
			output, err := testExec.ExecBash("~/.cargo/bin/sd --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("sd " + versions.SdVersion))
		})

		It("records sd tool state in state.json", func() {
			By("Reading state.json")
			output, err := testExec.ExecBash("cat ~/.local/share/toto/state.json")
			Expect(err).NotTo(HaveOccurred())

			By("Checking sd is in tools section")
			Expect(output).To(ContainSubstring(`"sd"`))

			By("Checking runtimeRef is recorded")
			Expect(output).To(ContainSubstring(`"runtimeRef": "rust"`))
		})
	})

	Context("Rust Delegation Idempotency", func() {
		It("is idempotent on subsequent applies", func() {
			By("Running toto apply again")
			_, err := testExec.Exec("toto", "apply", "~/delegation-test/")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying rustc still works")
			output, err := testExec.ExecBash("~/.cargo/bin/rustc --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("rustc"))

			By("Verifying sd still works")
			output, err = testExec.ExecBash("~/.cargo/bin/sd --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("sd " + versions.SdVersion))
		})
	})
}
