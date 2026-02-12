package tomei

// #RustRuntime declares a Rust runtime installed via rustup delegation.
// Defaults to "stable" toolchain.
//
// Usage:
//   rustRuntime: #RustRuntime
//   rustRuntime: #RustRuntime & {spec: version: "nightly"}
#RustRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Runtime"
	metadata: {
		name:        "rust"
		description: string | *"Rust programming language runtime via rustup"
	}
	spec: {
		type:    "delegation"
		version: string | *"stable"
		bootstrap: {
			install:        "curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain {{.Version}}"
			check:          "~/.cargo/bin/rustc --version"
			remove:         "~/.cargo/bin/rustup self uninstall -y"
			resolveVersion: "~/.cargo/bin/rustc --version 2>/dev/null | grep -oP '\\d+\\.\\d+\\.\\d+' || echo ''"
		}
		binaries: ["rustc", "cargo", "rustup"]
		binDir:      "~/.cargo/bin"
		toolBinPath: "~/.cargo/bin"
		env: {
			CARGO_HOME:  "~/.cargo"
			RUSTUP_HOME: "~/.rustup"
		}
		commands: {
			install: "~/.cargo/bin/cargo install {{.Package}}{{if .Version}}@{{.Version}}{{end}}"
			remove:  "rm -f {{.BinPath}}"
		}
	}
}

// #CargoBinstall declares the cargo-binstall tool (installed via cargo install).
// This tool is a prerequisite for #BinstallInstaller.
//
// Usage:
//   cargoBinstall: #CargoBinstall
#CargoBinstall: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: {
		name:        "cargo-binstall"
		description: string | *"Binary installation for Rust tools"
	}
	spec: {
		runtimeRef: "rust"
		package:    "cargo-binstall"
	}
}

// #BinstallInstaller declares the binstall delegation installer.
// Depends on #CargoBinstall being present.
//
// Usage:
//   binstallInstaller: #BinstallInstaller
#BinstallInstaller: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Installer"
	metadata: {
		name:        "binstall"
		description: string | *"Install pre-built Rust binaries via cargo-binstall"
	}
	spec: {
		type:    "delegation"
		toolRef: "cargo-binstall"
		commands: {
			install: "~/.cargo/bin/cargo-binstall {{.Package}}{{if .Version}}@{{.Version}}{{end}} --no-confirm"
			remove:  "rm -f {{.BinPath}}"
		}
	}
}

// #BinstallToolSet declares a set of tools installed via cargo-binstall.
// Requires #CargoBinstall and #BinstallInstaller to be declared.
//
// Usage:
//   rustTools: #BinstallToolSet & {
//       metadata: name: "rust-tools"
//       spec: tools: {
//           eza:       {package: "eza"}
//           hyperfine: {package: "hyperfine"}
//       }
//   }
#BinstallToolSet: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "ToolSet"
	metadata: {
		name:        string
		description: string | *"Rust tools installed via cargo-binstall"
	}
	spec: {
		installerRef: "binstall"
		tools: {[string]: {
			package:  string & !=""
			version?: string
		}}
	}
}
