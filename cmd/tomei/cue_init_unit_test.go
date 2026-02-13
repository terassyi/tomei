package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateModuleCUE_TableDriven(t *testing.T) {
	tests := []struct {
		name         string
		moduleName   string
		wantContains []string
	}{
		{
			name:       "default module name",
			moduleName: defaultModuleName,
			wantContains: []string{
				`module: "manifests.local@v0"`,
				`language: version: "v0.9.0"`,
				`"tomei.terassyi.net@v0": v: "v0.0.1"`,
			},
		},
		{
			name:       "custom module name",
			moduleName: "myproject@v0",
			wantContains: []string{
				`module: "myproject@v0"`,
				`language: version: "v0.9.0"`,
				`"tomei.terassyi.net@v0"`,
			},
		},
		{
			name:       "module name with dots",
			moduleName: "example.com/myapp@v0",
			wantContains: []string{
				`module: "example.com/myapp@v0"`,
				`language: version:`,
				`deps:`,
			},
		},
		{
			name:       "output contains language version and deps",
			moduleName: "test@v0",
			wantContains: []string{
				`language: version: "v0.9.0"`,
				`deps: {`,
				`"tomei.terassyi.net@v0": v: "v0.0.1"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := string(generateModuleCUE(tt.moduleName))
			for _, want := range tt.wantContains {
				assert.Contains(t, content, want)
			}
		})
	}
}

func TestGeneratePlatformCUE_TableDriven(t *testing.T) {
	content := string(generatePlatformCUE())

	wantContains := []string{
		"package tomei",
		`_os:       string @tag(os)`,
		`_arch:     string @tag(arch)`,
		`_headless: bool | *false @tag(headless,type=bool)`,
	}

	for _, want := range wantContains {
		assert.Contains(t, content, want)
	}
}

func TestRelativePath(t *testing.T) {
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
			got := relativePath(tt.base, tt.target)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestWriteFileIfAllowed(t *testing.T) {
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
			dir := t.TempDir()
			tt.setup(t, dir)

			var targetPath string
			if tt.name == "creates parent directories" {
				targetPath = filepath.Join(dir, "sub", "dir", "test.cue")
			} else {
				targetPath = filepath.Join(dir, "test.cue")
			}

			content := []byte("new content")
			err := writeFileIfAllowed(targetPath, content, tt.force)

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
