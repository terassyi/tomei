//go:build e2e

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func systemPackageTests() {
	const cfgPath = "~/system-package-test/"

	It("validates SystemPackage and SystemPackageSet manifests", func() {
		out, err := testExec.Exec("tomei", "validate", cfgPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("SystemInstaller/apt"))
		// SystemPackage は ExpandSets で展開され、出力は SystemPackageSet 名となる。
		Expect(out).To(ContainSubstring("SystemPackageSet/tree"))
		Expect(out).To(ContainSubstring("SystemPackageSet/cli-tools"))
		// desugar 契約: pre-expand の SystemPackage 名は出力に現れない。
		Expect(out).NotTo(ContainSubstring("SystemPackage/tree"))
		Expect(out).To(ContainSubstring("Validation successful"))
	})

	It("plan without --system shows system resources (skipped)", func() {
		// Action label (skip/install) is intentionally not asserted: today
		// SystemPackageSet shows "skip" via plan.go's skip-stub branch and will
		// become "install" once #199 wires the concrete APT installer.
		out, err := testExec.Exec("tomei", "plan", cfgPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("SystemPackageSet/tree"))
		Expect(out).To(ContainSubstring("SystemPackageSet/cli-tools"))
	})

	It("plan --system shows expanded SystemPackageSet entries", func() {
		out, err := testExec.Exec("tomei", "plan", "--system", cfgPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("SystemInstaller/apt"))
		Expect(out).To(ContainSubstring("SystemPackageSet/tree"))
		Expect(out).To(ContainSubstring("SystemPackageSet/cli-tools"))
	})
}
