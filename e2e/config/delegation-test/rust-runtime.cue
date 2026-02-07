package toto

// Rust runtime for E2E testing (delegation pattern via rustup)
// Uses rustup to bootstrap the Rust toolchain, then cargo install for tools.

_rustVersion: "stable"

rustRuntime: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind:       "Runtime"
	metadata: name: "rust"
	spec: {
		type:    "delegation"
		version: _rustVersion
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
			install: "~/.cargo/bin/cargo install {{.Package}}@{{.Version}}"
			remove:  "rm -f {{.BinPath}}"
		}
	}
}
