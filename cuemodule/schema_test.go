package cuemodule_test

import (
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/tomei/cuemodule"
)

// compileSchema compiles the schema CUE source and returns the context it was
// compiled in, so callers can compile test values in the same context.
func compileSchema(t *testing.T) (*cue.Context, cue.Value) {
	t.Helper()
	ctx := cuecontext.New()
	v := ctx.CompileString(cuemodule.SchemaCUE)
	require.NoError(t, v.Err(), "schema must compile without error")
	return ctx, v
}

func TestSchema_Compiles(t *testing.T) {
	compileSchema(t)
}

func TestSchema_Definitions_Exist(t *testing.T) {
	_, v := compileSchema(t)

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
		"#SystemPackage",
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
	ctx, v := compileSchema(t)
	resourceDef := v.LookupPath(cue.ParsePath("#Resource"))
	require.True(t, resourceDef.Exists())

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
			name: "Runtime with taintOnUpgrade",
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
					toolBinPath: "~/go/bin"
					taintOnUpgrade: true
				}
			}`,
		},
		{
			name: "Runtime download without toolBinPath and without commands",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Runtime"
				metadata: name: "lua"
				spec: {
					type:    "download"
					version: "5.4.7"
					source: {
						url: "https://www.lua.org/ftp/lua-5.4.7.tar.gz"
					}
					binaries: ["lua", "luac"]
				}
			}`,
		},
		{
			name: "Runtime delegation without toolBinPath and without commands",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Runtime"
				metadata: name: "lua-delegation"
				spec: {
					type:    "delegation"
					version: "5.4.7"
					bootstrap: {
						install: ["luaver install 5.4.7"]
						check:   ["lua -v"]
					}
				}
			}`,
		},
		{
			name: "Runtime delegation with commands and toolBinPath",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Runtime"
				metadata: name: "rust"
				spec: {
					type:    "delegation"
					version: "stable"
					bootstrap: {
						install: ["curl -sSf https://sh.rustup.rs | sh"]
						check:   ["rustc --version"]
					}
					toolBinPath: "~/.cargo/bin"
					commands: {
						install: ["cargo install {{.Package}}"]
					}
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
			name: "Installer with minimumReleaseAge",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Installer"
				metadata: name: "aqua"
				spec: {
					type: "download"
					minimumReleaseAge: "168h"
				}
			}`,
		},
		{
			// Schema accepts arbitrary strings — semantic validation is
			// deferred to Go-side InstallerSpec.Validate(). Proves the
			// schema layer does not duplicate the duration parser.
			name: "Installer with semantically invalid minimumReleaseAge string",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Installer"
				metadata: name: "download"
				spec: {
					type: "download"
					minimumReleaseAge: "notaduration"
				}
			}`,
		},
		{
			name: "Runtime with minimumReleaseAge",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Runtime"
				metadata: name: "go"
				spec: {
					type:        "download"
					version:     "1.25.6"
					toolBinPath: "~/go/bin"
					source: url: "https://go.dev/dl/go1.25.6.linux-amd64.tar.gz"
					minimumReleaseAge: "168h"
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
			// Locks the #ToolSet.spec for-comprehension that forces
			// runtimeRef: "go" when any tool item carries sha. The valid
			// case here passes runtimeRef: "go" explicitly so unification
			// is a no-op; the InvalidResources counterpart sets a
			// different runtimeRef and expects rejection.
			name: "ToolSet with sha in tool item",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "ToolSet"
				metadata: name: "go-tools"
				spec: {
					runtimeRef: "go"
					tools: {
						gopls: {
							package: "golang.org/x/tools/gopls"
							sha:     "0123456789abcdef0123456789abcdef01234567"
						}
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
			name: "SystemPackageRepository minimal apt",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "SystemPackageRepository"
				metadata: name: "docker"
				spec: {
					installerRef: "apt"
					apt: {
						url:        "https://download.docker.com/linux/ubuntu"
						keyUrl:     "https://download.docker.com/linux/ubuntu/gpg"
						keyHash:    "sha256:1500c1f56fa9e26b9b8f42452a553675796ade0807cdce11975eb98170b3a570"
						suite:      "jammy"
						components: ["stable"]
					}
				}
			}`,
		},
		{
			name: "SystemPackageRepository with allowed options",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "SystemPackageRepository"
				metadata: name: "docker"
				spec: {
					installerRef: "apt"
					apt: {
						url:        "https://download.docker.com/linux/ubuntu"
						keyUrl:     "https://download.docker.com/linux/ubuntu/gpg"
						keyHash:    "sha256:1500c1f56fa9e26b9b8f42452a553675796ade0807cdce11975eb98170b3a570"
						suite:      "jammy"
						components: ["stable"]
						options: {
							arch:      "amd64"
							"by-hash": "yes"
						}
					}
				}
			}`,
		},
		{
			name: "SystemPackage minimal",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "SystemPackage"
				metadata: name: "git"
				spec: {
					installerRef: "apt"
					package:      "git"
				}
			}`,
		},
		{
			name: "SystemPackage with repositoryRef and metadata description",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "SystemPackage"
				metadata: {
					name:        "docker"
					description: "Docker CE from upstream apt repository"
				}
				spec: {
					installerRef: "apt"
					repositoryRef: "docker"
					package:       "docker-ce"
				}
			}`,
		},
		{
			// Asserts schema admits per-installer grammar (e.g. apt multiarch `libc6:i386`) without false rejection.
			name: "SystemPackage with multiarch package identifier",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "SystemPackage"
				metadata: name: "libc6-i386"
				spec: {
					installerRef: "apt"
					package:      "libc6:i386"
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
	ctx, v := compileSchema(t)
	resourceDef := v.LookupPath(cue.ParsePath("#Resource"))
	require.True(t, resourceDef.Exists())

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
			name: "Runtime with commands but without toolBinPath",
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
					commands: {
						install: ["go install {{.Package}}@{{.Version}}"]
					}
				}
			}`,
		},
		{
			name: "Runtime delegation with commands but without toolBinPath",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Runtime"
				metadata: name: "rust"
				spec: {
					type:    "delegation"
					version: "stable"
					bootstrap: {
						install: ["curl -sSf https://sh.rustup.rs | sh"]
						check:   ["rustc --version"]
					}
					commands: {
						install: ["cargo install {{.Package}}"]
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
		{
			name: "Installer minimumReleaseAge not a string",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Installer"
				metadata: name: "download"
				spec: {
					type:              "download"
					minimumReleaseAge: 168
				}
			}`,
		},
		{
			name: "Runtime minimumReleaseAge not a string",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Runtime"
				metadata: name: "go"
				spec: {
					type:              "download"
					version:           "1.25.6"
					toolBinPath:       "~/go/bin"
					source: url:       "https://go.dev/dl/go1.25.6.linux-amd64.tar.gz"
					minimumReleaseAge: 168
				}
			}`,
		},
		{
			name: "SystemPackageSet with empty packages",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "SystemPackageSet"
				metadata: name: "cli-tools"
				spec: {
					installerRef: "apt"
					packages: []
				}
			}`,
		},
		{
			name: "SystemPackageSet element with embedded whitespace",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "SystemPackageSet"
				metadata: name: "cli-tools"
				spec: {
					installerRef: "apt"
					packages: ["git ", "curl"]
				}
			}`,
		},
		{
			name: "SystemPackageSet empty repositoryRef",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "SystemPackageSet"
				metadata: name: "cli-tools"
				spec: {
					installerRef:  "apt"
					repositoryRef: ""
					packages: ["git"]
				}
			}`,
		},
		{
			// Discriminator must literally be "apt"; CUE pins the union arm.
			name: "SystemPackageRepository wrong installerRef case",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "SystemPackageRepository"
				metadata: name: "docker"
				spec: {
					installerRef: "Apt"
					apt: {
						url:        "https://download.docker.com/linux/ubuntu"
						keyUrl:     "https://download.docker.com/linux/ubuntu/gpg"
						keyHash:    "sha256:1500c1f56fa9e26b9b8f42452a553675796ade0807cdce11975eb98170b3a570"
						suite:      "jammy"
						components: ["stable"]
					}
				}
			}`,
		},
		{
			name: "SystemPackageRepository missing apt block",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "SystemPackageRepository"
				metadata: name: "docker"
				spec: {
					installerRef: "apt"
				}
			}`,
		},
		{
			name: "SystemPackageRepository empty components",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "SystemPackageRepository"
				metadata: name: "docker"
				spec: {
					installerRef: "apt"
					apt: {
						url:        "https://download.docker.com/linux/ubuntu"
						keyUrl:     "https://download.docker.com/linux/ubuntu/gpg"
						keyHash:    "sha256:1500c1f56fa9e26b9b8f42452a553675796ade0807cdce11975eb98170b3a570"
						suite:      "jammy"
						components: []
					}
				}
			}`,
		},
		{
			// HTTPS-only constraint on key URL.
			name: "SystemPackageRepository non-HTTPS keyUrl",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "SystemPackageRepository"
				metadata: name: "docker"
				spec: {
					installerRef: "apt"
					apt: {
						url:        "https://download.docker.com/linux/ubuntu"
						keyUrl:     "http://download.docker.com/linux/ubuntu/gpg"
						keyHash:    "sha256:1500c1f56fa9e26b9b8f42452a553675796ade0807cdce11975eb98170b3a570"
						suite:      "jammy"
						components: ["stable"]
					}
				}
			}`,
		},
		{
			// keyHash must match the sha256:<64-hex> pattern; an md5 prefix or
			// short hash is rejected.
			name: "SystemPackageRepository malformed keyHash",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "SystemPackageRepository"
				metadata: name: "docker"
				spec: {
					installerRef: "apt"
					apt: {
						url:        "https://download.docker.com/linux/ubuntu"
						keyUrl:     "https://download.docker.com/linux/ubuntu/gpg"
						keyHash:    "md5:abc"
						suite:      "jammy"
						components: ["stable"]
					}
				}
			}`,
		},
		{
			// Flat repository syntax — APT's canonical form is `deb URL ./`
			// where the suite token is the literal "./" — is explicitly out
			// of scope. The CUE constraint rejects every flat-style marker
			// (./, /, ., ..) so manifests cannot smuggle a flat layout past
			// validate-time by varying the spelling.
			name: "SystemPackageRepository flat repo suite dotslash rejected",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "SystemPackageRepository"
				metadata: name: "vendor"
				spec: {
					installerRef: "apt"
					apt: {
						url:        "https://example.com/repo"
						keyUrl:     "https://example.com/repo/gpg"
						keyHash:    "sha256:0000000000000000000000000000000000000000000000000000000000000000"
						suite:      "./"
						components: ["main"]
					}
				}
			}`,
		},
		{
			name: "SystemPackageRepository flat repo suite slash rejected",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "SystemPackageRepository"
				metadata: name: "vendor"
				spec: {
					installerRef: "apt"
					apt: {
						url:        "https://example.com/repo"
						keyUrl:     "https://example.com/repo/gpg"
						keyHash:    "sha256:0000000000000000000000000000000000000000000000000000000000000000"
						suite:      "/"
						components: ["main"]
					}
				}
			}`,
		},
		{
			// trusted=yes disables signature verification — must be rejected
			// at the CUE layer so tomei validate catches it.
			name: "SystemPackageRepository trusted=yes option rejected",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "SystemPackageRepository"
				metadata: name: "vendor"
				spec: {
					installerRef: "apt"
					apt: {
						url:        "https://example.com/repo"
						keyUrl:     "https://example.com/repo/gpg"
						keyHash:    "sha256:0000000000000000000000000000000000000000000000000000000000000000"
						suite:      "stable"
						components: ["main"]
						options: { trusted: "yes" }
					}
				}
			}`,
		},
		{
			// signed-by must not be set; the keyring path is auto-derived
			// from metadata.name and the install/emit pair owns one source
			// of truth for the path.
			name: "SystemPackageRepository signed-by option rejected",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "SystemPackageRepository"
				metadata: name: "vendor"
				spec: {
					installerRef: "apt"
					apt: {
						url:        "https://example.com/repo"
						keyUrl:     "https://example.com/repo/gpg"
						keyHash:    "sha256:0000000000000000000000000000000000000000000000000000000000000000"
						suite:      "stable"
						components: ["main"]
						options: { "signed-by": "/etc/apt/keyrings/vendor.gpg" }
					}
				}
			}`,
		},
		{
			// allow-insecure is equivalent to trusted=yes in effect — also rejected.
			name: "SystemPackageRepository allow-insecure rejected",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "SystemPackageRepository"
				metadata: name: "vendor"
				spec: {
					installerRef: "apt"
					apt: {
						url:        "https://example.com/repo"
						keyUrl:     "https://example.com/repo/gpg"
						keyHash:    "sha256:0000000000000000000000000000000000000000000000000000000000000000"
						suite:      "stable"
						components: ["main"]
						options: { "allow-insecure": "yes" }
					}
				}
			}`,
		},
		{
			name: "SystemPackage without installerRef",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "SystemPackage"
				metadata: name: "git"
				spec: {
					package: "git"
				}
			}`,
		},
		{
			name: "SystemPackage empty installerRef",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "SystemPackage"
				metadata: name: "git"
				spec: {
					installerRef: ""
					package:      "git"
				}
			}`,
		},
		{
			name: "SystemPackage without package",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "SystemPackage"
				metadata: name: "git"
				spec: {
					installerRef: "apt"
				}
			}`,
		},
		{
			name: "SystemPackage empty package",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "SystemPackage"
				metadata: name: "git"
				spec: {
					installerRef: "apt"
					package:      ""
				}
			}`,
		},
		{
			name: "SystemPackage package with embedded space",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "SystemPackage"
				metadata: name: "git"
				spec: {
					installerRef: "apt"
					package:      "git vim"
				}
			}`,
		},
		{
			name: "SystemPackage package with trailing newline",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "SystemPackage"
				metadata: name: "git"
				spec: {
					installerRef: "apt"
					package:      "git\n"
				}
			}`,
		},
		{
			name: "SystemPackage empty repositoryRef",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "SystemPackage"
				metadata: name: "git"
				spec: {
					installerRef: "apt"
					repositoryRef: ""
					package:       "git"
				}
			}`,
		},
		{
			// Tool with sha set + installerRef ≠ "" must fail at the CUE
			// layer (installerRef?: =~"^$" forbids any non-empty installer).
			name: "Tool with sha and installerRef rejected",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Tool"
				metadata: name: "gopls"
				spec: {
					installerRef: "aqua"
					sha:          "0123456789abcdef0123456789abcdef01234567"
					package: {owner: "x", repo: "y"}
				}
			}`,
		},
		{
			// Tool with sha set + commands defined must fail at the CUE
			// layer (commands?: null forbids any non-null commands).
			name: "Tool with sha and commands rejected",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "Tool"
				metadata: name: "gopls"
				spec: {
					sha: "0123456789abcdef0123456789abcdef01234567"
					commands: install: ["true"]
				}
			}`,
		},
		{
			// ToolSet sha-in-tool-item + runtimeRef ≠ "go" must fail at
			// the CUE layer via the for-comprehension constraint.
			name: "ToolSet with sha in tool item but runtimeRef cargo rejected",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "ToolSet"
				metadata: name: "mixed-tools"
				spec: {
					runtimeRef: "cargo"
					tools: {
						gopls: {
							package: "golang.org/x/tools/gopls"
							sha:     "0123456789abcdef0123456789abcdef01234567"
						}
					}
				}
			}`,
		},
		{
			// ToolSet sha-in-tool-item + installerRef set must fail at the
			// CUE layer — mirrors #Tool.spec's installerRef forbiddance so
			// a ToolSet with sha-pinned tools cannot also delegate to an
			// installer.
			name: "ToolSet with sha in tool item and installerRef rejected",
			cue: `{
				apiVersion: "tomei.terassyi.net/v1beta1"
				kind:       "ToolSet"
				metadata: name: "mixed-tools"
				spec: {
					runtimeRef:  "go"
					installerRef: "aqua"
					tools: {
						gopls: {
							package: "golang.org/x/tools/gopls"
							sha:     "0123456789abcdef0123456789abcdef01234567"
						}
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
