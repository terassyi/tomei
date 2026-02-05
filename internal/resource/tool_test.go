package resource

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolSpec_Validate(t *testing.T) {
	tests := []struct {
		name    string
		spec    *ToolSpec
		wantErr string
	}{
		{
			name: "valid download pattern with source",
			spec: &ToolSpec{
				InstallerRef: "aqua",
				Version:      "1.0.0",
				Source: &DownloadSource{
					URL: "https://example.com/tool.tar.gz",
				},
			},
			wantErr: "",
		},
		{
			name: "valid registry pattern",
			spec: &ToolSpec{
				InstallerRef: "aqua",
				Version:      "2.86.0",
				Package:      &Package{Owner: "cli", Repo: "cli"},
			},
			wantErr: "",
		},
		{
			name: "valid registry pattern without version (latest)",
			spec: &ToolSpec{
				InstallerRef: "aqua",
				Package:      &Package{Owner: "cli", Repo: "cli"},
			},
			wantErr: "",
		},
		{
			name: "valid delegation pattern",
			spec: &ToolSpec{
				RuntimeRef: "go",
				Package:    &Package{Name: "golang.org/x/tools/gopls"},
			},
			wantErr: "",
		},
		{
			name: "valid delegation pattern with version",
			spec: &ToolSpec{
				RuntimeRef: "go",
				Version:    "v0.16.0",
				Package:    &Package{Name: "golang.org/x/tools/gopls"},
			},
			wantErr: "",
		},
		{
			name:    "missing installerRef and runtimeRef",
			spec:    &ToolSpec{Version: "1.0.0"},
			wantErr: "either installerRef or runtimeRef is required",
		},
		{
			name: "missing version, source, and package",
			spec: &ToolSpec{
				InstallerRef: "aqua",
			},
			wantErr: "version, source, or package is required",
		},
		{
			name: "runtimeRef without package",
			spec: &ToolSpec{
				RuntimeRef: "go",
				Version:    "1.0.0",
			},
			wantErr: "package is required when using runtimeRef",
		},
		{
			name: "runtimeRef with registry package (valid - uses Name for delegation)",
			spec: &ToolSpec{
				RuntimeRef: "go",
				Package:    &Package{Name: "golang.org/x/tools/gopls"},
				Version:    "v0.21.0",
			},
			wantErr: "",
		},
		{
			name: "source and package both specified",
			spec: &ToolSpec{
				InstallerRef: "aqua",
				Version:      "1.0.0",
				Package:      &Package{Owner: "cli", Repo: "cli"},
				Source: &DownloadSource{
					URL: "https://example.com/tool.tar.gz",
				},
			},
			wantErr: "cannot specify both source and package",
		},
		{
			name: "registry package with non-aqua installer",
			spec: &ToolSpec{
				InstallerRef: "brew",
				Package:      &Package{Owner: "cli", Repo: "cli"},
			},
			wantErr: "package with owner/repo requires installerRef: aqua",
		},
		{
			name: "package missing repo when owner specified",
			spec: &ToolSpec{
				InstallerRef: "aqua",
				Package:      &Package{Owner: "cli"},
			},
			wantErr: "package.repo is required when owner is specified",
		},
		{
			name: "package missing owner when repo specified",
			spec: &ToolSpec{
				InstallerRef: "aqua",
				Package:      &Package{Repo: "cli"},
			},
			wantErr: "package.owner is required when repo is specified",
		},
		{
			name: "package with both registry and name (invalid)",
			spec: &ToolSpec{
				InstallerRef: "aqua",
				Package:      &Package{Owner: "cli", Repo: "cli", Name: "example"},
			},
			wantErr: "cannot specify both owner/repo and name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.spec.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestPackage_Validate(t *testing.T) {
	tests := []struct {
		name    string
		pkg     *Package
		wantErr string
	}{
		{
			name:    "valid registry package",
			pkg:     &Package{Owner: "cli", Repo: "cli"},
			wantErr: "",
		},
		{
			name:    "valid name package",
			pkg:     &Package{Name: "golang.org/x/tools/gopls"},
			wantErr: "",
		},
		{
			name:    "nil package is valid",
			pkg:     nil,
			wantErr: "",
		},
		{
			name:    "empty package is valid",
			pkg:     &Package{},
			wantErr: "",
		},
		{
			name:    "owner without repo",
			pkg:     &Package{Owner: "cli"},
			wantErr: "package.repo is required when owner is specified",
		},
		{
			name:    "repo without owner",
			pkg:     &Package{Repo: "cli"},
			wantErr: "package.owner is required when repo is specified",
		},
		{
			name:    "both registry and name",
			pkg:     &Package{Owner: "cli", Repo: "cli", Name: "example"},
			wantErr: "cannot specify both owner/repo and name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.pkg.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestPackage_String(t *testing.T) {
	tests := []struct {
		name string
		pkg  *Package
		want string
	}{
		{
			name: "registry package",
			pkg:  &Package{Owner: "cli", Repo: "cli"},
			want: "cli/cli",
		},
		{
			name: "name package",
			pkg:  &Package{Name: "golang.org/x/tools/gopls"},
			want: "golang.org/x/tools/gopls",
		},
		{
			name: "nil package",
			pkg:  nil,
			want: "",
		},
		{
			name: "empty package",
			pkg:  &Package{},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.pkg.String())
		})
	}
}

func TestPackage_IsEmpty(t *testing.T) {
	tests := []struct {
		name string
		pkg  *Package
		want bool
	}{
		{
			name: "nil is empty",
			pkg:  nil,
			want: true,
		},
		{
			name: "all fields empty is empty",
			pkg:  &Package{},
			want: true,
		},
		{
			name: "only owner is not empty",
			pkg:  &Package{Owner: "cli"},
			want: false,
		},
		{
			name: "only repo is not empty",
			pkg:  &Package{Repo: "cli"},
			want: false,
		},
		{
			name: "only name is not empty",
			pkg:  &Package{Name: "example"},
			want: false,
		},
		{
			name: "registry package is not empty",
			pkg:  &Package{Owner: "cli", Repo: "cli"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.pkg.IsEmpty())
		})
	}
}

func TestPackage_IsRegistry(t *testing.T) {
	tests := []struct {
		name string
		pkg  *Package
		want bool
	}{
		{
			name: "registry package",
			pkg:  &Package{Owner: "cli", Repo: "cli"},
			want: true,
		},
		{
			name: "name package",
			pkg:  &Package{Name: "example"},
			want: false,
		},
		{
			name: "nil package",
			pkg:  nil,
			want: false,
		},
		{
			name: "empty package",
			pkg:  &Package{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.pkg.IsRegistry())
		})
	}
}

func TestPackage_IsName(t *testing.T) {
	tests := []struct {
		name string
		pkg  *Package
		want bool
	}{
		{
			name: "name package",
			pkg:  &Package{Name: "example"},
			want: true,
		},
		{
			name: "registry package",
			pkg:  &Package{Owner: "cli", Repo: "cli"},
			want: false,
		},
		{
			name: "nil package",
			pkg:  nil,
			want: false,
		},
		{
			name: "empty package",
			pkg:  &Package{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.pkg.IsName())
		})
	}
}

func TestToolSpec_Dependencies(t *testing.T) {
	tests := []struct {
		name string
		spec *ToolSpec
		want []Ref
	}{
		{
			name: "installerRef only",
			spec: &ToolSpec{
				InstallerRef: "aqua",
				Version:      "1.0.0",
			},
			want: []Ref{{Kind: KindInstaller, Name: "aqua"}},
		},
		{
			name: "runtimeRef only",
			spec: &ToolSpec{
				RuntimeRef: "go",
				Package:    &Package{Name: "example.com/tool"},
			},
			want: []Ref{{Kind: KindRuntime, Name: "go"}},
		},
		{
			name: "both installerRef and runtimeRef",
			spec: &ToolSpec{
				InstallerRef: "aqua",
				RuntimeRef:   "go",
				Package:      &Package{Name: "example.com/tool"},
			},
			want: []Ref{
				{Kind: KindInstaller, Name: "aqua"},
				{Kind: KindRuntime, Name: "go"},
			},
		},
		{
			name: "neither (empty deps)",
			spec: &ToolSpec{
				Version: "1.0.0",
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.spec.Dependencies()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPackage_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		want    *Package
		wantErr bool
	}{
		// Registry format strings (owner/repo) - auto-parsed to Owner+Repo
		{
			name: "string owner/repo format - cli/cli",
			json: `"cli/cli"`,
			want: &Package{Owner: "cli", Repo: "cli"},
		},
		{
			name: "string owner/repo format - BurntSushi/ripgrep",
			json: `"BurntSushi/ripgrep"`,
			want: &Package{Owner: "BurntSushi", Repo: "ripgrep"},
		},
		{
			name: "string owner/repo format - sharkdp/fd",
			json: `"sharkdp/fd"`,
			want: &Package{Owner: "sharkdp", Repo: "fd"},
		},
		{
			name: "string owner/repo format - jqlang/jq",
			json: `"jqlang/jq"`,
			want: &Package{Owner: "jqlang", Repo: "jq"},
		},

		// Name format strings (with dots or multiple slashes) - stored as Name
		{
			name: "string with go package path",
			json: `"golang.org/x/tools/gopls"`,
			want: &Package{Name: "golang.org/x/tools/gopls"},
		},
		{
			name: "string with domain",
			json: `"github.com/user/repo"`,
			want: &Package{Name: "github.com/user/repo"},
		},
		{
			name: "string with simple name (no slash)",
			json: `"ripgrep"`,
			want: &Package{Name: "ripgrep"},
		},
		{
			name: "string with @scope npm package",
			json: `"@biomejs/biome"`,
			want: &Package{Name: "@biomejs/biome"},
		},

		// Object format
		{
			name: "object with owner/repo",
			json: `{"owner": "BurntSushi", "repo": "ripgrep"}`,
			want: &Package{Owner: "BurntSushi", Repo: "ripgrep"},
		},
		{
			name: "object with name",
			json: `{"name": "golang.org/x/tools/gopls"}`,
			want: &Package{Name: "golang.org/x/tools/gopls"},
		},

		// Error cases
		{
			name:    "invalid json",
			json:    `{invalid}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Package
			err := got.UnmarshalJSON([]byte(tt.json))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, &got)
		})
	}
}

func TestIsRegistryFormat(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// Registry format (owner/repo)
		{"cli/cli", true},
		{"BurntSushi/ripgrep", true},
		{"sharkdp/fd", true},
		{"jqlang/jq", true},
		{"user/repo", true},

		// Not registry format - has dots before slash
		{"golang.org/x/tools/gopls", false},
		{"github.com/user/repo", false},
		{"example.com/pkg", false},

		// Not registry format - multiple slashes
		{"a/b/c", false},
		{"org/repo/subpkg", false},

		// Not registry format - no slash
		{"ripgrep", false},
		{"fd", false},
		{"", false},

		// Not registry format - starts with @
		{"@biomejs/biome", false},

		// Edge cases
		{"/repo", false},  // empty owner
		{"owner/", false}, // empty repo
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isRegistryFormat(tt.input)
			assert.Equal(t, tt.want, got, "isRegistryFormat(%q)", tt.input)
		})
	}
}
