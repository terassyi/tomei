package resource

import (
	"fmt"
	"strings"
)

// minReleaseAgeVarRef is the template variable a delegation install command
// must reference for its declared minimumReleaseAge to be honored (#253). Shown
// in the warning as the canonical form users should write.
const minReleaseAgeVarRef = "{{.MinimumReleaseAge}}"

// minReleaseAgeFieldRef is the substring actually matched. Matching the field
// reference rather than the full delimited action tolerates legitimate template
// forms — `{{ .MinimumReleaseAge }}`, `{{- .MinimumReleaseAge -}}`,
// `{{.MinimumReleaseAge | quote}}` — that a delimiter-literal match would miss.
const minReleaseAgeFieldRef = ".MinimumReleaseAge"

// LintMinimumReleaseAge returns advisory (non-fatal) warnings for delegation
// Installers and Runtimes that declare minimumReleaseAge but whose install
// command(s) never reference {{.MinimumReleaseAge}} — i.e. a supply-chain
// policy that is declared but cannot be enforced (tomei does not gate
// delegation/runtime install paths; #253 only exposes the value as a template
// variable, and the user's command must honor it).
//
// Download-type Installers (builtin aqua/download) are excluded: tomei enforces
// those itself at apply time (#254). Runtimes are linted regardless of type
// because tomei never gates runtime installs — the template var is the only
// enforcement path. Warnings are returned in resource iteration order.
func LintMinimumReleaseAge(resources []Resource) []string {
	var warnings []string
	for _, res := range resources {
		switch r := res.(type) {
		case *Installer:
			if r.InstallerSpec == nil || !r.InstallerSpec.Type.IsDelegation() {
				continue
			}
			if d, err := r.InstallerSpec.ParsedMinimumReleaseAge(); err != nil || d == 0 {
				continue
			}
			if !referencesMinReleaseAge(installerInstallCommands(r.InstallerSpec)) {
				warnings = append(warnings, lintWarning(KindInstaller, r.Name()))
			}
		case *Runtime:
			if r.RuntimeSpec == nil {
				continue
			}
			if d, err := r.RuntimeSpec.ParsedMinimumReleaseAge(); err != nil || d == 0 {
				continue
			}
			if !referencesMinReleaseAge(runtimeInstallCommands(r.RuntimeSpec)) {
				warnings = append(warnings, lintWarning(KindRuntime, r.Name()))
			}
		}
	}
	return warnings
}

func lintWarning(kind Kind, name string) string {
	return fmt.Sprintf("%s/%s declares minimumReleaseAge but no install command references %s; the policy is not enforced",
		kind, name, minReleaseAgeVarRef)
}

// referencesMinReleaseAge reports whether any of the given command strings
// references the {{.MinimumReleaseAge}} template variable, tolerating spacing,
// trim markers, and pipelines by matching the field reference.
func referencesMinReleaseAge(cmds []string) bool {
	return strings.Contains(strings.Join(cmds, "\n"), minReleaseAgeFieldRef)
}

func installerInstallCommands(spec *InstallerSpec) []string {
	if spec.Commands == nil {
		return nil
	}
	return spec.Commands.Install
}

// runtimeInstallCommands gathers every install command the runtime exposes the
// variable to: tool-install (Commands.Install) and delegation self-install
// (Bootstrap.Install). Either referencing the var satisfies the lint.
func runtimeInstallCommands(spec *RuntimeSpec) []string {
	var cmds []string
	if spec.Commands != nil {
		cmds = append(cmds, spec.Commands.Install...)
	}
	if spec.Bootstrap != nil {
		cmds = append(cmds, spec.Bootstrap.Install...)
	}
	return cmds
}
