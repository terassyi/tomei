package cuemodule_test

import (
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/tomei/cuemodule"
)

// compileSchema compiles the schema CUE source.
func compileSchema(t *testing.T) cue.Value {
	t.Helper()
	ctx := cuecontext.New()
	v := ctx.CompileString(cuemodule.SchemaCUE)
	require.NoError(t, v.Err(), "schema must compile without error")
	return v
}

func TestSchema_Compiles(t *testing.T) {
	compileSchema(t)
}

func TestSchema_Definitions_Exist(t *testing.T) {
	v := compileSchema(t)

	definitions := []string{
		"#APIVersion",
		"#Metadata",
		"#HTTPSURL",
		"#Checksum",
		"#DownloadSource",
		"#CommandSet",
		"#Package",
		"#Runtime",
		"#Installer",
		"#InstallerRepository",
		"#Tool",
		"#ToolSet",
		"#SystemInstaller",
		"#SystemPackageRepository",
		"#SystemPackageSet",
		"#Resource",
	}

	for _, def := range definitions {
		t.Run(def, func(t *testing.T) {
			d := v.LookupPath(cue.ParsePath(def))
			assert.True(t, d.Exists(), "definition %s should exist", def)
		})
	}
}

func TestSchema_ValidResources(t *testing.T) {
	v := compileSchema(t)
	resourceDef := v.LookupPath(cue.ParsePath("#Resource"))
	require.True(t, resourceDef.Exists())
	ctx := v.Context()

	tests := []struct {
		name string
		cue  string
	}{
		{
			name: "Tool with installerRef and source",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Tool"
				metadata: name: "ripgrep"
				spec: {
					installerRef: "download"
					version:      "14.0.0"
					source: {
						url: "https://github.com/BurntSushi/ripgrep/releases/download/14.0.0/ripgrep-14.0.0.tar.gz"
					}
				}
			}`,
		},
		{
			name: "Tool with runtimeRef and package",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Tool"
				metadata: name: "gopls"
				spec: {
					runtimeRef: "go"
					package:    "golang.org/x/tools/gopls"
					version:    "v0.21.0"
				}
			}`,
		},
		{
			name: "Tool with package object",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Tool"
				metadata: name: "gh"
				spec: {
					installerRef: "aqua"
					version:      "2.86.0"
					package: {
						owner: "cli"
						repo:  "cli"
					}
				}
			}`,
		},
		{
			name: "Runtime download type",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Runtime"
				metadata: name: "go"
				spec: {
					type:    "download"
					version: "1.25.6"
					source: {
						url: "https://go.dev/dl/go1.25.6.linux-amd64.tar.gz"
					}
					binaries:    ["go", "gofmt"]
					toolBinPath: "~/go/bin"
					commands: {
						install: ["go install {{.Package}}@{{.Version}}"]
						remove:  ["rm -f {{.BinPath}}"]
					}
					env: {
						GOROOT: "~/.local/share/tomei/runtimes/go/1.25.6"
					}
				}
			}`,
		},
		{
			name: "Runtime delegation type",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Runtime"
				metadata: name: "rust"
				spec: {
					type:    "delegation"
					version: "stable"
					bootstrap: {
						install: ["curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y"]
						check:   ["rustc --version"]
						remove:  ["rustup self uninstall -y"]
						resolveVersion: ["rustc --version | grep -oP '[0-9]+\\.[0-9]+\\.[0-9]+'"]
					}
					binaries:    ["rustc", "cargo", "rustup"]
					toolBinPath: "~/.cargo/bin"
				}
			}`,
		},
		{
			name: "Installer download type",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Installer"
				metadata: name: "download"
				spec: {
					type: "download"
				}
			}`,
		},
		{
			name: "Installer delegation type",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Installer"
				metadata: name: "go"
				spec: {
					type:       "delegation"
					runtimeRef: "go"
					commands: {
						install: ["go install {{.Package}}@{{.Version}}"]
						check:   ["go version -m {{.BinPath}}"]
						remove:  ["rm {{.BinPath}}"]
					}
				}
			}`,
		},
		{
			name: "InstallerRepository delegation",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "InstallerRepository"
				metadata: name: "bitnami"
				spec: {
					installerRef: "helm"
					source: {
						type: "delegation"
						commands: {
							install: ["helm repo add bitnami https://charts.bitnami.com/bitnami"]
							check:   ["helm repo list | grep bitnami"]
							remove:  ["helm repo remove bitnami"]
						}
					}
				}
			}`,
		},
		{
			name: "InstallerRepository git",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "InstallerRepository"
				metadata: name: "custom-registry"
				spec: {
					installerRef: "aqua"
					source: {
						type: "git"
						url:  "https://github.com/my-org/aqua-registry"
					}
				}
			}`,
		},
		{
			name: "ToolSet",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "ToolSet"
				metadata: name: "go-tools"
				spec: {
					runtimeRef: "go"
					tools: {
						staticcheck: {package: "honnef.co/go/tools/cmd/staticcheck", version: "v0.6.0"}
						godoc:       {package: "golang.org/x/tools/cmd/godoc", version: "v0.31.0"}
					}
				}
			}`,
		},
		{
			name: "SystemPackageSet",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "SystemPackageSet"
				metadata: name: "cli-tools"
				spec: {
					installerRef: "apt"
					packages: ["jq", "curl", "htop"]
				}
			}`,
		},
		{
			name: "Tool with metadata description",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Tool"
				metadata: {
					name:        "ripgrep"
					description: "A fast line-oriented search tool"
				}
				spec: {
					installerRef: "download"
					version:      "14.0.0"
				}
			}`,
		},
		{
			name: "Tool with metadata description and labels",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Tool"
				metadata: {
					name:        "ripgrep"
					description: "A fast line-oriented search tool"
					labels: {
						category: "search"
					}
				}
				spec: {
					installerRef: "download"
					version:      "14.0.0"
				}
			}`,
		},
		{
			name: "Tool with enabled false",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Tool"
				metadata: name: "disabled-tool"
				spec: {
					installerRef: "download"
					version:      "1.0.0"
					enabled:      false
				}
			}`,
		},
		{
			name: "Tool with checksum value",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Tool"
				metadata: name: "verified-tool"
				spec: {
					installerRef: "download"
					version:      "1.0.0"
					source: {
						url: "https://example.com/tool.tar.gz"
						checksum: {
							value: "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
						}
						archiveType: "tar.gz"
					}
				}
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := ctx.CompileString(tt.cue)
			require.NoError(t, res.Err(), "test CUE must compile")

			unified := res.Unify(resourceDef)
			err := unified.Validate(cue.Concrete(true))
			assert.NoError(t, err, "valid resource should pass schema validation")
		})
	}
}

func TestSchema_InvalidResources(t *testing.T) {
	v := compileSchema(t)
	resourceDef := v.LookupPath(cue.ParsePath("#Resource"))
	require.True(t, resourceDef.Exists())
	ctx := v.Context()

	tests := []struct {
		name string
		cue  string
	}{
		{
			name: "wrong apiVersion",
			cue: `{
				apiVersion: "wrong/v1"
				kind:       "Tool"
				metadata: name: "test"
				spec: {
					installerRef: "download"
					version:      "1.0.0"
				}
			}`,
		},
		{
			name: "invalid kind",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "InvalidKind"
				metadata: name: "test"
				spec: {}
			}`,
		},
		{
			name: "non-HTTPS URL in source",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Tool"
				metadata: name: "test"
				spec: {
					installerRef: "download"
					version:      "1.0.0"
					source: {
						url: "http://example.com/tool.tar.gz"
					}
				}
			}`,
		},
		{
			name: "invalid checksum format",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Tool"
				metadata: name: "test"
				spec: {
					installerRef: "download"
					version:      "1.0.0"
					source: {
						url: "https://example.com/tool.tar.gz"
						checksum: {
							value: "md5:abc123"
						}
					}
				}
			}`,
		},
		{
			name: "Runtime download without source",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Runtime"
				metadata: name: "go"
				spec: {
					type:        "download"
					version:     "1.25.6"
					toolBinPath: "~/go/bin"
				}
			}`,
		},
		{
			name: "Runtime delegation without bootstrap",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Runtime"
				metadata: name: "rust"
				spec: {
					type:        "delegation"
					version:     "stable"
					toolBinPath: "~/.cargo/bin"
				}
			}`,
		},
		{
			name: "Runtime empty version",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Runtime"
				metadata: name: "go"
				spec: {
					type:        "download"
					version:     ""
					toolBinPath: "~/go/bin"
					source: {
						url: "https://go.dev/dl/go.tar.gz"
					}
				}
			}`,
		},
		{
			name: "Installer delegation without commands",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Installer"
				metadata: name: "test"
				spec: {
					type: "delegation"
				}
			}`,
		},
		{
			name: "InstallerRepository delegation without commands",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "InstallerRepository"
				metadata: name: "test"
				spec: {
					installerRef: "helm"
					source: {
						type: "delegation"
					}
				}
			}`,
		},
		{
			name: "InstallerRepository git without url",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "InstallerRepository"
				metadata: name: "test"
				spec: {
					installerRef: "aqua"
					source: {
						type: "git"
					}
				}
			}`,
		},
		{
			name: "invalid archive type",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Tool"
				metadata: name: "test"
				spec: {
					installerRef: "download"
					version:      "1.0.0"
					source: {
						url:         "https://example.com/tool.gz"
						archiveType: "gzip"
					}
				}
			}`,
		},
		{
			name: "invalid InstallerRepository source type",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "InstallerRepository"
				metadata: name: "test"
				spec: {
					installerRef: "test"
					source: {
						type: "invalid"
					}
				}
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := ctx.CompileString(tt.cue)
			require.NoError(t, res.Err(), "test CUE must compile")

			unified := res.Unify(resourceDef)
			err := unified.Validate(cue.Concrete(true))
			assert.Error(t, err, "invalid resource should fail schema validation")
		})
	}
}
