package e2e_test

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Dependency Resolution", Ordered, func() {
	var configDir string

	BeforeAll(func() {
		containerName = os.Getenv("TOTO_E2E_CONTAINER")
		if containerName == "" {
			Skip("TOTO_E2E_CONTAINER environment variable is not set - skipping E2E tests")
		}

		// Create config directory for dependency tests
		_, err := containerExecBash("mkdir -p ~/dependency-test")
		Expect(err).NotTo(HaveOccurred())
		configDir = "~/dependency-test"

		// Initialize toto (may already be initialized by other tests, ignore errors)
		_, _ = containerExec("toto", "init", "--yes")
	})

	AfterAll(func() {
		// Cleanup
		_, _ = containerExecBash("rm -rf ~/dependency-test")
	})

	Describe("Circular Dependency Detection", func() {
		BeforeEach(func() {
			// Clean up config files before each test
			_, _ = containerExecBash("rm -f " + configDir + "/*.cue")
		})

		It("detects circular dependency between installer and tool", func() {
			By("Creating config with circular dependency: Installer(a) -> Tool(b) -> Installer(a)")
			cueContent := `package toto

installerA: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "installer-a"
	spec: {
		type: "delegation"
		toolRef: "tool-b"
		commands: {
			install: "installer-a install {{.Package}}"
		}
	}
}

toolB: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "tool-b"
	spec: {
		installerRef: "installer-a"
		version: "1.0.0"
	}
}
`
			_, err := containerExecBash("cat > " + configDir + "/circular.cue << 'EOF'\n" + cueContent + "EOF")
			Expect(err).NotTo(HaveOccurred())

			By("Running toto validate - should detect cycle")
			output, err := containerExec("toto", "validate", configDir)
			Expect(err).To(HaveOccurred())
			Expect(output).To(ContainSubstring("circular dependency"))
		})

		It("detects circular dependency in three-node cycle", func() {
			By("Creating config with 3-node cycle: A -> B -> C -> A")
			cueContent := `package toto

toolA: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "tool-a"
	spec: {
		installerRef: "installer-c"
		version: "1.0.0"
	}
}

installerB: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "installer-b"
	spec: {
		type: "delegation"
		toolRef: "tool-a"
		commands: { install: "echo install" }
	}
}

toolC: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "tool-c"
	spec: {
		installerRef: "installer-b"
		version: "1.0.0"
	}
}

installerC: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "installer-c"
	spec: {
		type: "delegation"
		toolRef: "tool-c"
		commands: { install: "echo install" }
	}
}
`
			_, err := containerExecBash("cat > " + configDir + "/circular3.cue << 'EOF'\n" + cueContent + "EOF")
			Expect(err).NotTo(HaveOccurred())

			By("Running toto validate - should detect cycle")
			output, err := containerExec("toto", "validate", configDir)
			Expect(err).To(HaveOccurred())
			Expect(output).To(ContainSubstring("circular dependency"))
		})

		It("rejects installer with both runtimeRef and toolRef", func() {
			By("Creating config with invalid installer")
			cueContent := `package toto

invalidInstaller: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "invalid"
	spec: {
		type: "delegation"
		runtimeRef: "go"
		toolRef: "some-tool"
		commands: { install: "echo install" }
	}
}
`
			_, err := containerExecBash("cat > " + configDir + "/invalid.cue << 'EOF'\n" + cueContent + "EOF")
			Expect(err).NotTo(HaveOccurred())

			By("Running toto validate - should reject")
			output, err := containerExec("toto", "validate", configDir)
			Expect(err).To(HaveOccurred())
			Expect(output).To(ContainSubstring("runtimeRef"))
			Expect(output).To(ContainSubstring("toolRef"))
		})
	})

	Describe("Parallel Tool Installation", func() {
		BeforeEach(func() {
			_, _ = containerExecBash("rm -f " + configDir + "/*.cue")
			// Reset state.json to clean state (keep directories)
			_, _ = containerExecBash(`echo '{"runtimes":{},"tools":{},"installers":{}}' > ~/.local/share/toto/state.json`)
		})

		It("installs multiple independent tools in parallel", func() {
			By("Creating config with multiple independent tools")
			cueContent := `package toto

aquaInstaller: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "aqua"
	spec: {
		type: "download"
	}
}

rg: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "rg"
	spec: {
		installerRef: "aqua"
		version: "14.1.1"
		source: {
			url: "https://github.com/BurntSushi/ripgrep/releases/download/14.1.1/ripgrep-14.1.1-aarch64-unknown-linux-gnu.tar.gz"
			checksum: {
				value: "sha256:c827481c4ff4ea10c9dc7a4022c8de5db34a5737cb74484d62eb94a95841ab2f"
			}
		}
	}
}

fd: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "fd"
	spec: {
		installerRef: "aqua"
		version: "10.2.0"
		source: {
			url: "https://github.com/sharkdp/fd/releases/download/v10.2.0/fd-v10.2.0-aarch64-unknown-linux-gnu.tar.gz"
			checksum: {
				value: "sha256:6de8be7a3d8ca27954a6d1e22bc327af4cf6fc7622791e68b820197f915c422b"
			}
		}
	}
}

bat: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "bat"
	spec: {
		installerRef: "aqua"
		version: "0.24.0"
		source: {
			url: "https://github.com/sharkdp/bat/releases/download/v0.24.0/bat-v0.24.0-aarch64-unknown-linux-gnu.tar.gz"
			checksum: {
				value: "sha256:feccae9a0576d97609c57e32d3914c5116136eab0df74c2ab74ef397d42c5b10"
			}
		}
	}
}
`
			_, err := containerExecBash("cat > " + configDir + "/parallel.cue << 'EOF'\n" + cueContent + "EOF")
			Expect(err).NotTo(HaveOccurred())

			By("Running toto validate")
			output, err := containerExec("toto", "validate", configDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Validation successful"))

			By("Running toto apply")
			output, err = containerExec("toto", "apply", configDir)
			Expect(err).NotTo(HaveOccurred())

			By("Checking all tools were installed")
			Expect(output).To(ContainSubstring("name=rg"))
			Expect(output).To(ContainSubstring("name=fd"))
			Expect(output).To(ContainSubstring("name=bat"))

			By("Verifying rg works")
			output, err = containerExecBash("~/.local/bin/rg --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("ripgrep 14.1.1"))

			By("Verifying fd works")
			output, err = containerExecBash("~/.local/bin/fd --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("fd 10.2.0"))

			By("Verifying bat works")
			output, err = containerExecBash("~/.local/bin/bat --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("bat 0.24.0"))
		})

		It("is idempotent - second apply reports no changes", func() {
			By("Running toto apply again")
			output, err := containerExec("toto", "apply", configDir)
			Expect(err).NotTo(HaveOccurred())

			By("Checking no changes needed")
			Expect(output).To(SatisfyAny(
				ContainSubstring("no changes"),
				ContainSubstring("total_actions=0"),
			))
		})
	})

	Describe("Tool as Installer Dependency (ToolRef)", func() {
		var toolRefCueContent string

		BeforeAll(func() {
			toolRefCueContent = `package toto

// Base installer (download pattern)
aquaInstaller: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "aqua"
	spec: {
		type: "download"
	}
}

// jq tool installed via aqua
jqTool: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "jq"
	spec: {
		installerRef: "aqua"
		version: "1.7.1"
		source: {
			url: "https://github.com/jqlang/jq/releases/download/jq-1.7.1/jq-linux-arm64"
			checksum: {
				value: "sha256:4dd2d8a0661df0b22f1bb9a1f9830f06b6f3b8f7d91211a1ef5d7c4f06a8b4a5"
			}
			archiveType: "raw"
		}
	}
}

// jq-based installer - depends on jq tool via toolRef
jqInstaller: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "jq-installer"
	spec: {
		type: "delegation"
		toolRef: "jq"
		commands: {
			install: "jq --version && echo 'Installing {{.Package}} via jq-installer'"
		}
	}
}
`
			// Clean up and create config
			_, _ = containerExecBash("rm -f " + configDir + "/*.cue")
			_, _ = containerExecBash(`echo '{"runtimes":{},"tools":{},"installers":{}}' > ~/.local/share/toto/state.json`)
			_, err := containerExecBash("cat > " + configDir + "/toolref.cue << 'EOF'\n" + toolRefCueContent + "EOF")
			Expect(err).NotTo(HaveOccurred())
		})

		It("validates toolRef dependency chain", func() {
			By("Running toto validate")
			output, err := containerExec("toto", "validate", configDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Validation successful"))

			By("Checking dependency order in output")
			Expect(output).To(ContainSubstring("Installer/aqua"))
			Expect(output).To(ContainSubstring("Tool/jq"))
			Expect(output).To(ContainSubstring("Installer/jq-installer"))
		})

		It("installs tool before dependent installer is available", func() {
			By("Running toto apply")
			output, err := containerExec("toto", "apply", configDir)
			Expect(err).NotTo(HaveOccurred())

			By("Checking jq tool was installed")
			Expect(output).To(ContainSubstring("name=jq"))

			By("Verifying jq is installed and works")
			output, err = containerExecBash("~/.local/bin/jq --version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("jq-1.7"))
		})
	})

	Describe("Runtime to Tool Dependency Chain", func() {
		var runtimeChainCueContent string

		BeforeAll(func() {
			runtimeChainCueContent = `package toto

// Go Runtime
goRuntime: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		type: "download"
		version: "1.23.5"
		source: {
			url: "https://go.dev/dl/go1.23.5.linux-arm64.tar.gz"
			checksum: {
				url: "https://go.dev/dl/?mode=json&include=all"
			}
		}
		binaries: ["go", "gofmt"]
		binDir: "~/go/bin"
		toolBinPath: "~/go/bin"
		env: {
			GOROOT: "~/.local/share/toto/runtimes/go/1.23.5"
			GOBIN: "~/go/bin"
		}
		commands: {
			install: "go install {{.Package}}@{{.Version}}"
			remove: "rm -f {{.BinPath}}"
		}
	}
}

// Go Installer - depends on Go Runtime via runtimeRef
goInstaller: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "go"
	spec: {
		type: "delegation"
		runtimeRef: "go"
		commands: {
			install: "go install {{.Package}}{{if .Version}}@{{.Version}}{{end}}"
		}
	}
}

// gopls Tool - installed via go installer
gopls: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "gopls"
	spec: {
		installerRef: "go"
		runtimeRef: "go"
		package: "golang.org/x/tools/gopls"
		version: "v0.17.1"
	}
}
`
			// Clean up and create config
			_, _ = containerExecBash("rm -f " + configDir + "/*.cue")
			_, _ = containerExecBash(`echo '{"runtimes":{},"tools":{},"installers":{}}' > ~/.local/share/toto/state.json`)
			_, err := containerExecBash("cat > " + configDir + "/runtime-chain.cue << 'EOF'\n" + runtimeChainCueContent + "EOF")
			Expect(err).NotTo(HaveOccurred())
		})

		It("validates runtime -> installer -> tool chain", func() {
			By("Running toto validate")
			output, err := containerExec("toto", "validate", configDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("Validation successful"))

			By("Checking all resources recognized")
			Expect(output).To(ContainSubstring("Runtime/go"))
			Expect(output).To(ContainSubstring("Installer/go"))
			Expect(output).To(ContainSubstring("Tool/gopls"))
		})

		It("installs runtime before dependent tools", func() {
			By("Running toto apply")
			output, err := containerExec("toto", "apply", configDir)
			Expect(err).NotTo(HaveOccurred())

			By("Checking runtime was installed")
			Expect(output).To(ContainSubstring("installing runtime"))
			Expect(output).To(ContainSubstring("name=go"))

			By("Checking tool was installed after runtime")
			Expect(output).To(ContainSubstring("installing tool"))
			Expect(output).To(ContainSubstring("name=gopls"))

			By("Verifying go runtime 1.23.5 is installed in expected location")
			// Set GOTOOLCHAIN=local to prevent auto-upgrade to newer Go version
			output, err = containerExecBash("GOTOOLCHAIN=local ~/.local/share/toto/runtimes/go/1.23.5/bin/go version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("go1.23"))

			By("Verifying gopls is installed")
			output, err = containerExecBash("~/go/bin/gopls version")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("golang.org/x/tools/gopls"))
		})
	})
})
