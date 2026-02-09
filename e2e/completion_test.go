//go:build e2e

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func completionTests() {

	It("generates bash completion script", func() {
		output, err := testExec.Exec("tomei", "completion", "bash")
		Expect(err).NotTo(HaveOccurred())

		By("Checking bash completion markers")
		Expect(output).To(ContainSubstring("bash completion V2"))
		Expect(output).To(ContainSubstring("__start_tomei"))
	})

	It("generates zsh completion script", func() {
		output, err := testExec.Exec("tomei", "completion", "zsh")
		Expect(err).NotTo(HaveOccurred())

		By("Checking zsh completion markers")
		Expect(output).To(ContainSubstring("compdef"))
		Expect(output).To(ContainSubstring("_tomei"))
	})

	It("generates fish completion script", func() {
		output, err := testExec.Exec("tomei", "completion", "fish")
		Expect(err).NotTo(HaveOccurred())

		By("Checking fish completion markers")
		Expect(output).To(ContainSubstring("complete"))
		Expect(output).To(ContainSubstring("tomei"))
	})

	It("generates powershell completion script", func() {
		output, err := testExec.Exec("tomei", "completion", "powershell")
		Expect(err).NotTo(HaveOccurred())

		By("Checking powershell completion markers")
		Expect(output).To(ContainSubstring("Register-ArgumentCompleter"))
		Expect(output).To(ContainSubstring("tomei"))
	})

	It("rejects invalid shell argument", func() {
		_, err := testExec.Exec("tomei", "completion", "invalid")
		Expect(err).To(HaveOccurred())
	})

	It("rejects missing shell argument", func() {
		_, err := testExec.Exec("tomei", "completion")
		Expect(err).To(HaveOccurred())
	})
}
