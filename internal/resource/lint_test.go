package resource

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func delegationInstaller(minAge string, install []string) *Installer {
	return &Installer{
		BaseResource: BaseResource{APIVersion: GroupVersion, ResourceKind: KindInstaller, Metadata: Metadata{Name: "brew"}},
		InstallerSpec: &InstallerSpec{
			Type:              InstallTypeDelegation,
			MinimumReleaseAge: minAge,
			Commands:          &CommandsSpec{Install: install},
		},
	}
}

func runtimeRes(name, typ, minAge string, commands []string, bootstrap []string) *Runtime {
	spec := &RuntimeSpec{
		Type:              InstallType(typ),
		Version:           "1.0.0",
		MinimumReleaseAge: minAge,
	}
	if commands != nil {
		spec.Commands = &CommandsSpec{Install: commands}
	}
	if bootstrap != nil {
		spec.Bootstrap = &RuntimeBootstrapSpec{CommandSet: CommandSet{Install: bootstrap}}
	}
	return &Runtime{
		BaseResource: BaseResource{APIVersion: GroupVersion, ResourceKind: KindRuntime, Metadata: Metadata{Name: name}},
		RuntimeSpec:  spec,
	}
}

func TestLintMinimumReleaseAge_Installer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		res      Resource
		wantWarn bool
	}{
		{"delegation missing ref", delegationInstaller("168h", []string{"brew install {{.Name}}"}), true},
		{"delegation present ref", delegationInstaller("168h", []string{"brew install --min-age={{.MinimumReleaseAge}} {{.Name}}"}), false},
		{"delegation whitespace variant", delegationInstaller("168h", []string{"x {{ .MinimumReleaseAge }}"}), false},
		{"delegation trim-marker variant", delegationInstaller("168h", []string{"x {{- .MinimumReleaseAge -}}"}), false},
		{"delegation pipeline variant", delegationInstaller("168h", []string{`x {{.MinimumReleaseAge | printf "%s"}}`}), false},
		{"delegation no threshold", delegationInstaller("", []string{"brew install {{.Name}}"}), false},
		{"download type excluded", &Installer{
			BaseResource:  BaseResource{ResourceKind: KindInstaller, Metadata: Metadata{Name: "aqua"}},
			InstallerSpec: &InstallerSpec{Type: InstallTypeDownload, MinimumReleaseAge: "168h"},
		}, false},
		{"delegation nil commands + threshold", &Installer{
			BaseResource:  BaseResource{ResourceKind: KindInstaller, Metadata: Metadata{Name: "brew"}},
			InstallerSpec: &InstallerSpec{Type: InstallTypeDelegation, MinimumReleaseAge: "168h"},
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			warns := LintMinimumReleaseAge([]Resource{tt.res})
			if tt.wantWarn {
				require.Len(t, warns, 1)
				assert.Contains(t, warns[0], "minimumReleaseAge")
			} else {
				assert.Empty(t, warns)
			}
		})
	}
}

// Ordering contract: warnings are returned in resource iteration order, and a
// non-Installer/Runtime resource (Tool) is ignored.
func TestLintMinimumReleaseAge_MultipleInIterationOrder(t *testing.T) {
	t.Parallel()
	resources := []Resource{
		delegationInstaller("168h", []string{"brew install {{.Name}}"}),                                                       // bad → warns (Installer/brew)
		&Tool{BaseResource: BaseResource{ResourceKind: KindTool, Metadata: Metadata{Name: "ignored"}}, ToolSpec: &ToolSpec{}}, // ignored
		runtimeRes("go", "delegation", "168h", []string{"go install x"}, []string{"boot"}),                                    // bad → warns (Runtime/go)
	}
	warns := LintMinimumReleaseAge(resources)
	require.Len(t, warns, 2)
	assert.Contains(t, warns[0], "Installer/brew")
	assert.Contains(t, warns[1], "Runtime/go")
}

func TestLintMinimumReleaseAge_Runtime(t *testing.T) {
	t.Parallel()
	ref := []string{"go install {{.Package}} {{.MinimumReleaseAge}}"}
	noRef := []string{"go install {{.Package}}"}
	tests := []struct {
		name     string
		res      Resource
		wantWarn bool
	}{
		{"neither refs", runtimeRes("go", "delegation", "168h", noRef, noRef), true},
		{"ref in commands only", runtimeRes("go", "delegation", "168h", ref, noRef), false},
		{"ref in bootstrap only", runtimeRes("rust", "delegation", "168h", noRef, ref), false},
		{"download type, commands ref", runtimeRes("go", "download", "168h", ref, nil), false},
		{"download type, commands no-ref + threshold", runtimeRes("go", "download", "168h", noRef, nil), true},
		{"no threshold", runtimeRes("go", "delegation", "", noRef, noRef), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			warns := LintMinimumReleaseAge([]Resource{tt.res})
			if tt.wantWarn {
				require.Len(t, warns, 1)
			} else {
				assert.Empty(t, warns)
			}
		})
	}
}
