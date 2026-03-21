package cuemod

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"cuelang.org/go/mod/modregistrytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/config"
)

func TestGenerateModuleCUE_TableDriven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		moduleName    string
		moduleVersion string
		wantContains  []string
	}{
		{
			name:          "default module name and version",
			moduleName:    DefaultModuleName,
			moduleVersion: DefaultModuleVer,
			wantContains: []string{
				`module: "manifests.local@v0"`,
				`language: version: "v0.9.0"`,
				`"tomei.terassyi.net@v0": v: "v0.1.9"`,
			},
		},
		{
			name:          "custom module name",
			moduleName:    "myproject@v0",
			moduleVersion: "v0.0.1",
			wantContains: []string{
				`module: "myproject@v0"`,
				`language: version: "v0.9.0"`,
				`"tomei.terassyi.net@v0"`,
			},
		},
		{
			name:          "custom version",
			moduleName:    DefaultModuleName,
			moduleVersion: "v0.0.3",
			wantContains: []string{
				`module: "manifests.local@v0"`,
				`"tomei.terassyi.net@v0": v: "v0.0.3"`,
			},
		},
		{
			name:          "module name with dots",
			moduleName:    "example.com/myapp@v0",
			moduleVersion: "v0.0.1",
			wantContains: []string{
				`module: "example.com/myapp@v0"`,
				`language: version:`,
				`deps:`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content, err := GenerateModuleCUE(tt.moduleName, tt.moduleVersion)
			require.NoError(t, err)
			for _, want := range tt.wantContains {
				assert.Contains(t, string(content), want)
			}
		})
	}
}

func TestGeneratePlatformCUE_TableDriven(t *testing.T) {
	t.Parallel()
	content, err := GeneratePlatformCUE()
	require.NoError(t, err)

	wantContains := []string{
		"package tomei",
		`_os:       string @tag(os)`,
		`_arch:     string @tag(arch)`,
		`_headless: bool | *false @tag(headless,type=bool)`,
	}

	for _, want := range wantContains {
		assert.Contains(t, string(content), want)
	}
}

func TestRelativePath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		base   string
		target string
		want   string
	}{
		{
			name:   "child path",
			base:   "/home/user/project",
			target: "/home/user/project/cue.mod/module.cue",
			want:   filepath.Join("cue.mod", "module.cue"),
		},
		{
			name:   "sibling path",
			base:   "/home/user/project",
			target: "/home/user/other/file.cue",
			want:   filepath.Join("..", "other", "file.cue"),
		},
		{
			name:   "same path",
			base:   "/home/user/project",
			target: "/home/user/project",
			want:   ".",
		},
		{
			name:   "direct child file",
			base:   "/home/user/project",
			target: "/home/user/project/tomei_platform.cue",
			want:   "tomei_platform.cue",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := RelativePath(tt.base, tt.target)
			assert.Equal(t, tt.want, got)
		})
	}
}

func setupMockRegistry(t *testing.T, versions ...string) *modregistrytest.Registry {
	t.Helper()
	var fs fstest.MapFS
	if len(versions) == 0 {
		fs = fstest.MapFS{}
	} else {
		fs = mergeMockModuleFS(versions...)
	}
	reg, err := modregistrytest.New(fs, "")
	require.NoError(t, err)
	t.Cleanup(reg.Close)
	t.Setenv(config.EnvCUERegistry, reg.Host()+"+insecure")
	return reg
}

func TestResolveLatestVersion(t *testing.T) {
	tests := []struct {
		name        string
		versions    []string
		opts        []ResolveOption
		want        string
		wantErr     bool
		errContains string
	}{
		{
			name:     "returns latest version from multiple",
			versions: []string{"v0.0.1", "v0.0.2", "v0.0.3"},
			want:     "v0.0.3",
		},
		{
			name:     "returns single version",
			versions: []string{"v0.0.1"},
			want:     "v0.0.1",
		},
		{
			name:    "error when no versions published",
			wantErr: true,
		},
		{
			name:     "skips pre-release versions",
			versions: []string{"v0.0.1", "v0.0.2", "v0.0.3-rc.1"},
			want:     "v0.0.2",
		},
		{
			name:     "includes pre-release when requested",
			versions: []string{"v0.0.1", "v0.0.2", "v0.0.3-rc.1"},
			opts:     []ResolveOption{WithPreRelease()},
			want:     "v0.0.3-rc.1",
		},
		{
			name:        "error when only pre-release versions exist",
			versions:    []string{"v0.0.1-alpha.1"},
			wantErr:     true,
			errContains: "no published versions found",
		},
		{
			name:     "returns pre-release when only pre-release and opted in",
			versions: []string{"v0.0.1-alpha.1"},
			opts:     []ResolveOption{WithPreRelease()},
			want:     "v0.0.1-alpha.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupMockRegistry(t, tt.versions...)

			version, err := ResolveLatestVersion(context.Background(), tt.opts...)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, version)
		})
	}
}

func TestWriteFileIfAllowed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		setup     func(t *testing.T, dir string)
		force     bool
		wantErr   bool
		errSubstr string
	}{
		{
			name:  "new file creates successfully",
			setup: func(_ *testing.T, _ string) {},
			force: false,
		},
		{
			name: "existing file force=false returns error",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "test.cue"), []byte("existing"), 0644))
			},
			force:     false,
			wantErr:   true,
			errSubstr: "already exists",
		},
		{
			name: "existing file force=true overwrites",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "test.cue"), []byte("old content"), 0644))
			},
			force: true,
		},
		{
			name:  "creates parent directories",
			setup: func(_ *testing.T, _ string) {},
			force: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			tt.setup(t, dir)

			var targetPath string
			if tt.name == "creates parent directories" {
				targetPath = filepath.Join(dir, "sub", "dir", "test.cue")
			} else {
				targetPath = filepath.Join(dir, "test.cue")
			}

			content := []byte("new content")
			err := WriteFileIfAllowed(targetPath, content, tt.force)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
				return
			}

			require.NoError(t, err)
			data, err := os.ReadFile(targetPath)
			require.NoError(t, err)
			assert.Equal(t, content, data)
		})
	}
}
