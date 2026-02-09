package printer

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/state"
)

// --- Helper / Utility tests ---

func TestResolveResourceType(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"tools", "tools", false},
		{"tool", "tools", false},
		{"runtimes", "runtimes", false},
		{"runtime", "runtimes", false},
		{"rt", "runtimes", false},
		{"installers", "installers", false},
		{"installer", "installers", false},
		{"inst", "installers", false},
		{"installerrepositories", "installerrepositories", false},
		{"installerrepository", "installerrepositories", false},
		{"instrepo", "installerrepositories", false},
		{"Tools", "tools", false},
		{"RUNTIMES", "runtimes", false},
		{"unknown", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ResolveResourceType(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFormatVersionKind(t *testing.T) {
	tests := []struct {
		name        string
		vk          resource.VersionKind
		specVersion string
		want        string
	}{
		{"exact", resource.VersionExact, "14.1.1", "exact"},
		{"latest", resource.VersionLatest, "", "latest"},
		{"alias", resource.VersionAlias, "stable", "alias(stable)"},
		{"alias lts", resource.VersionAlias, "lts", "alias(lts)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatVersionKind(tt.vk, tt.specVersion)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFilterMap(t *testing.T) {
	m := map[string]*resource.ToolState{
		"a": {Version: "1"},
		"b": {Version: "2"},
	}

	t.Run("no filter returns all", func(t *testing.T) {
		result := filterMap(m, "")
		assert.Len(t, result, 2)
	})

	t.Run("filter by name", func(t *testing.T) {
		result := filterMap(m, "a")
		assert.Len(t, result, 1)
		assert.Equal(t, "1", result["a"].Version)
	})

	t.Run("filter not found", func(t *testing.T) {
		result := filterMap(m, "c")
		assert.Nil(t, result)
	})
}

func TestSortedKeys(t *testing.T) {
	m := map[string]*resource.ToolState{
		"c": {},
		"a": {},
		"b": {},
	}

	keys := sortedKeys(m)
	assert.Equal(t, []string{"a", "b", "c"}, keys)
}

// --- Formatter unit tests ---

func TestToolFormatter_Headers(t *testing.T) {
	f := toolFormatter{}

	t.Run("normal", func(t *testing.T) {
		h := f.Headers(false)
		assert.Equal(t, []string{"NAME", "VERSION", "VERSION_KIND", "INSTALLER/RUNTIME"}, h)
	})

	t.Run("wide", func(t *testing.T) {
		h := f.Headers(true)
		assert.Equal(t, []string{"NAME", "VERSION", "VERSION_KIND", "INSTALLER/RUNTIME", "PACKAGE", "BIN_PATH"}, h)
	})
}

func TestToolFormatter_FormatRow(t *testing.T) {
	f := toolFormatter{}

	t.Run("installed with installerRef", func(t *testing.T) {
		ts := &resource.ToolState{
			Version:      "14.1.1",
			InstallerRef: "aqua",
			VersionKind:  resource.VersionExact,
			SpecVersion:  "14.1.1",
		}
		row := f.FormatRow("ripgrep", ts, false)
		assert.Equal(t, "ripgrep", row[0])
		assert.Equal(t, "14.1.1", row[1])
		assert.Equal(t, "exact", row[2])
		assert.Equal(t, "aqua", row[3])
		assert.Len(t, row, 4)
	})

	t.Run("with runtimeRef", func(t *testing.T) {
		ts := &resource.ToolState{
			Version:     "v0.17.0",
			RuntimeRef:  "go",
			VersionKind: resource.VersionLatest,
		}
		row := f.FormatRow("gopls", ts, false)
		assert.Equal(t, "go", row[3])
		assert.Len(t, row, 4)
	})

	t.Run("wide adds package and binpath", func(t *testing.T) {
		ts := &resource.ToolState{
			Version:      "14.1.1",
			InstallerRef: "aqua",
			VersionKind:  resource.VersionExact,
			BinPath:      "/home/user/.local/bin/rg",
			Package:      &resource.Package{Owner: "BurntSushi", Repo: "ripgrep"},
		}
		row := f.FormatRow("ripgrep", ts, true)
		assert.Len(t, row, 6)
		assert.Equal(t, "BurntSushi/ripgrep", row[4])
		assert.Equal(t, "/home/user/.local/bin/rg", row[5])
	})
}

func TestRuntimeFormatter_Headers(t *testing.T) {
	f := runtimeFormatter{}

	t.Run("normal", func(t *testing.T) {
		h := f.Headers(false)
		assert.Equal(t, []string{"NAME", "VERSION", "VERSION_KIND", "TYPE"}, h)
	})

	t.Run("wide", func(t *testing.T) {
		h := f.Headers(true)
		assert.Equal(t, []string{"NAME", "VERSION", "VERSION_KIND", "TYPE", "INSTALL_PATH", "BINARIES"}, h)
	})
}

func TestRuntimeFormatter_FormatRow(t *testing.T) {
	f := runtimeFormatter{}

	t.Run("download", func(t *testing.T) {
		rs := &resource.RuntimeState{
			Type:        resource.InstallTypeDownload,
			Version:     "1.25.1",
			VersionKind: resource.VersionExact,
		}
		row := f.FormatRow("go", rs, false)
		assert.Equal(t, []string{"go", "1.25.1", "exact", "download"}, row)
	})

	t.Run("delegation with alias", func(t *testing.T) {
		rs := &resource.RuntimeState{
			Type:        resource.InstallTypeDelegation,
			Version:     "1.75.0",
			VersionKind: resource.VersionAlias,
			SpecVersion: "stable",
		}
		row := f.FormatRow("rust", rs, false)
		assert.Equal(t, "alias(stable)", row[2])
		assert.Equal(t, "delegation", row[3])
	})

	t.Run("wide adds install_path and binaries", func(t *testing.T) {
		rs := &resource.RuntimeState{
			Type:        resource.InstallTypeDownload,
			Version:     "1.25.1",
			VersionKind: resource.VersionExact,
			InstallPath: "/home/user/.local/share/tomei/runtimes/go/1.25.1",
			Binaries:    []string{"go", "gofmt"},
		}
		row := f.FormatRow("go", rs, true)
		assert.Len(t, row, 6)
		assert.Equal(t, "/home/user/.local/share/tomei/runtimes/go/1.25.1", row[4])
		assert.Equal(t, "go,gofmt", row[5])
	})
}

func TestInstallerFormatter_FormatRow(t *testing.T) {
	f := installerFormatter{}

	t.Run("with toolRef", func(t *testing.T) {
		is := &resource.InstallerState{Version: "v1.10.0", ToolRef: "cargo-binstall"}
		row := f.FormatRow("binstall", is, false)
		assert.Equal(t, []string{"binstall", "v1.10.0", "cargo-binstall"}, row)
	})

	t.Run("without toolRef", func(t *testing.T) {
		is := &resource.InstallerState{Version: "v2.45.0"}
		row := f.FormatRow("aqua", is, false)
		assert.Equal(t, []string{"aqua", "v2.45.0", ""}, row)
	})
}

func TestInstallerRepoFormatter_FormatRow(t *testing.T) {
	f := installerRepoFormatter{}

	rs := &resource.InstallerRepositoryState{
		InstallerRef: "aqua",
		SourceType:   "git",
		URL:          "https://github.com/example/registry",
	}
	row := f.FormatRow("custom-registry", rs, false)
	assert.Equal(t, []string{"custom-registry", "aqua", "git", "https://github.com/example/registry"}, row)
}

// --- printTable integration tests ---

func TestPrintTable_Tools(t *testing.T) {
	buf := &bytes.Buffer{}
	tools := map[string]*resource.ToolState{
		"ripgrep": {
			Version:      "14.1.1",
			InstallerRef: "aqua",
			VersionKind:  resource.VersionExact,
			SpecVersion:  "14.1.1",
		},
		"gopls": {
			Version:     "v0.17.0",
			RuntimeRef:  "go",
			VersionKind: resource.VersionLatest,
			TaintReason: "runtime_upgraded",
		},
	}

	printTable(buf, tools, "", false, toolFormatter{})
	output := buf.String()

	// Header
	assert.Contains(t, output, "NAME")
	assert.Contains(t, output, "VERSION")
	assert.Contains(t, output, "INSTALLER/RUNTIME")
	assert.NotContains(t, output, "STATUS")

	// Rows sorted alphabetically (gopls before ripgrep)
	assert.Contains(t, output, "gopls")
	assert.Contains(t, output, "ripgrep")
	goplsIdx := bytes.Index([]byte(output), []byte("gopls"))
	ripgrepIdx := bytes.Index([]byte(output), []byte("ripgrep"))
	assert.Less(t, goplsIdx, ripgrepIdx)

	// Wide columns absent
	assert.NotContains(t, output, "PACKAGE")
	assert.NotContains(t, output, "BIN_PATH")
}

func TestPrintTable_Tools_Wide(t *testing.T) {
	buf := &bytes.Buffer{}
	tools := map[string]*resource.ToolState{
		"ripgrep": {
			Version:      "14.1.1",
			InstallerRef: "aqua",
			VersionKind:  resource.VersionExact,
			BinPath:      "/home/user/.local/bin/rg",
			Package:      &resource.Package{Owner: "BurntSushi", Repo: "ripgrep"},
		},
	}

	printTable(buf, tools, "", true, toolFormatter{})
	output := buf.String()

	assert.Contains(t, output, "PACKAGE")
	assert.Contains(t, output, "BIN_PATH")
	assert.Contains(t, output, "BurntSushi/ripgrep")
	assert.Contains(t, output, "/home/user/.local/bin/rg")
}

func TestPrintTable_Tools_NameFilter(t *testing.T) {
	buf := &bytes.Buffer{}
	tools := map[string]*resource.ToolState{
		"ripgrep": {Version: "14.1.1", InstallerRef: "aqua", VersionKind: resource.VersionExact},
		"gopls":   {Version: "v0.17.0", RuntimeRef: "go", VersionKind: resource.VersionLatest},
	}

	printTable(buf, tools, "ripgrep", false, toolFormatter{})
	output := buf.String()

	assert.Contains(t, output, "ripgrep")
	assert.NotContains(t, output, "gopls")
}

func TestPrintTable_Empty(t *testing.T) {
	buf := &bytes.Buffer{}

	printTable(buf, map[string]*resource.ToolState{}, "", false, toolFormatter{})

	assert.Contains(t, buf.String(), "No resources found.")
}

func TestPrintTable_NameFilter_NotFound(t *testing.T) {
	buf := &bytes.Buffer{}
	tools := map[string]*resource.ToolState{
		"ripgrep": {Version: "14.1.1", InstallerRef: "aqua"},
	}

	printTable(buf, tools, "nonexistent", false, toolFormatter{})

	assert.Contains(t, buf.String(), "No resources found.")
}

func TestPrintTable_Runtimes(t *testing.T) {
	buf := &bytes.Buffer{}
	runtimes := map[string]*resource.RuntimeState{
		"go": {
			Type:        resource.InstallTypeDownload,
			Version:     "1.25.1",
			VersionKind: resource.VersionExact,
		},
		"rust": {
			Type:        resource.InstallTypeDelegation,
			Version:     "1.75.0",
			VersionKind: resource.VersionAlias,
			SpecVersion: "stable",
		},
	}

	printTable(buf, runtimes, "", false, runtimeFormatter{})
	output := buf.String()

	assert.Contains(t, output, "NAME")
	assert.Contains(t, output, "TYPE")
	assert.Contains(t, output, "download")
	assert.Contains(t, output, "delegation")
	assert.Contains(t, output, "alias(stable)")
}

func TestPrintTable_Installers(t *testing.T) {
	buf := &bytes.Buffer{}
	installers := map[string]*resource.InstallerState{
		"aqua":     {Version: "v2.45.0"},
		"binstall": {Version: "v1.10.0", ToolRef: "cargo-binstall"},
	}

	printTable(buf, installers, "", false, installerFormatter{})
	output := buf.String()

	assert.Contains(t, output, "NAME")
	assert.Contains(t, output, "VERSION")
	assert.Contains(t, output, "TOOL_REF")
	assert.Contains(t, output, "aqua")
	assert.Contains(t, output, "cargo-binstall")
}

func TestPrintTable_InstallerRepositories(t *testing.T) {
	buf := &bytes.Buffer{}
	repos := map[string]*resource.InstallerRepositoryState{
		"custom-registry": {
			InstallerRef: "aqua",
			SourceType:   "git",
			URL:          "https://github.com/example/registry",
		},
	}

	printTable(buf, repos, "", false, installerRepoFormatter{})
	output := buf.String()

	assert.Contains(t, output, "INSTALLER")
	assert.Contains(t, output, "SOURCE_TYPE")
	assert.Contains(t, output, "custom-registry")
	assert.Contains(t, output, "https://github.com/example/registry")
}

// --- printResources tests ---

func TestPrintResources_JSON(t *testing.T) {
	buf := &bytes.Buffer{}
	tools := map[string]*resource.ToolState{
		"ripgrep": {Version: "14.1.1", InstallerRef: "aqua", VersionKind: resource.VersionExact},
	}

	err := printResources(buf, tools, "", false, true, toolFormatter{})
	require.NoError(t, err)

	var result map[string]*resource.ToolState
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "14.1.1", result["ripgrep"].Version)
}

func TestPrintResources_JSON_NameFilter(t *testing.T) {
	buf := &bytes.Buffer{}
	tools := map[string]*resource.ToolState{
		"ripgrep": {Version: "14.1.1", InstallerRef: "aqua"},
		"gopls":   {Version: "v0.17.0", RuntimeRef: "go"},
	}

	err := printResources(buf, tools, "ripgrep", false, true, toolFormatter{})
	require.NoError(t, err)

	var result map[string]*resource.ToolState
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Contains(t, result, "ripgrep")
}

func TestPrintResources_Table(t *testing.T) {
	buf := &bytes.Buffer{}
	tools := map[string]*resource.ToolState{
		"ripgrep": {Version: "14.1.1", InstallerRef: "aqua", VersionKind: resource.VersionExact},
	}

	err := printResources(buf, tools, "", false, false, toolFormatter{})
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "NAME")
	assert.Contains(t, output, "ripgrep")
}

// --- Run integration test ---

func TestRun(t *testing.T) {
	userState := &state.UserState{
		Tools: map[string]*resource.ToolState{
			"ripgrep": {Version: "14.1.1", InstallerRef: "aqua", VersionKind: resource.VersionExact},
		},
		Runtimes:              map[string]*resource.RuntimeState{},
		Installers:            map[string]*resource.InstallerState{},
		InstallerRepositories: map[string]*resource.InstallerRepositoryState{},
	}

	t.Run("tools table", func(t *testing.T) {
		buf := &bytes.Buffer{}
		err := Run(buf, userState, "tools", "", false, false)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "ripgrep")
	})

	t.Run("tools json", func(t *testing.T) {
		buf := &bytes.Buffer{}
		err := Run(buf, userState, "tools", "", false, true)
		require.NoError(t, err)

		var result map[string]*resource.ToolState
		err = json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Len(t, result, 1)
	})

	t.Run("empty runtimes", func(t *testing.T) {
		buf := &bytes.Buffer{}
		err := Run(buf, userState, "runtimes", "", false, false)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "No resources found.")
	})

	t.Run("unknown type", func(t *testing.T) {
		buf := &bytes.Buffer{}
		err := Run(buf, userState, "unknown", "", false, false)
		require.Error(t, err)
	})
}
