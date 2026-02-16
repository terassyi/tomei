package tomei

// Rust ecosystem: rustup delegation → cargo-binstall → tools via binstall
//
// Dependency chain:
//   Runtime/rust → Tool/cargo-binstall (cargo install)
//                → Installer/binstall (delegation, toolRef: cargo-binstall)
//                    → Tool/eza, Tool/hyperfine (installerRef: binstall)
//                → Tool/tokei (cargo install, runtimeRef: rust)

// ---------------------------------------------------------------------------
// Rust Runtime (delegation via rustup)
// ---------------------------------------------------------------------------

_rustVersion: "stable"

rustRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Runtime"
	metadata: name: "rust"
	spec: {
		type:    "delegation"
		version: _rustVersion
		bootstrap: {
			install: ["curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain {{.Version}}"]
			check: ["~/.cargo/bin/rustc --version"]
			remove: ["~/.cargo/bin/rustup self uninstall -y"]
			resolveVersion: ["~/.cargo/bin/rustc --version 2>/dev/null | grep -oP '\\d+\\.\\d+\\.\\d+' || echo ''"]
		}
		binaries: ["rustc", "cargo", "rustup"]
		binDir:      "~/.cargo/bin"
		toolBinPath: "~/.cargo/bin"
		env: {
			CARGO_HOME:  "~/.cargo"
			RUSTUP_HOME: "~/.rustup"
		}
		commands: {
			install: ["~/.cargo/bin/cargo install {{.Package}}{{if .Version}}@{{.Version}}{{end}}"]
			remove: ["rm -f {{.BinPath}}"]
		}
	}
}

// ---------------------------------------------------------------------------
// cargo-binstall — installed via cargo install, then used as an Installer
// ---------------------------------------------------------------------------

cargoBinstall: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "cargo-binstall"
	spec: {
		runtimeRef: "rust"
		package:    "cargo-binstall"
	}
}

// ---------------------------------------------------------------------------
// binstall Installer (delegation) — depends on cargo-binstall tool
// ---------------------------------------------------------------------------

binstallInstaller: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Installer"
	metadata: name: "binstall"
	spec: {
		type:    "delegation"
		toolRef: "cargo-binstall"
		commands: {
			install: ["~/.cargo/bin/cargo-binstall {{.Package}}{{if .Version}}@{{.Version}}{{end}} --no-confirm"]
			remove: ["rm -f {{.BinPath}}"]
		}
	}
}

// ---------------------------------------------------------------------------
// Tools installed via binstall (fast pre-built binary downloads)
// ---------------------------------------------------------------------------

eza: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "eza"
	spec: {
		installerRef: "binstall"
		package:      "eza"
	}
}

hyperfine: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "hyperfine"
	spec: {
		installerRef: "binstall"
		package:      "hyperfine"
	}
}

// ---------------------------------------------------------------------------
// Tool installed via cargo install (source compilation, runtimeRef)
// ---------------------------------------------------------------------------

tokei: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "tokei"
	spec: {
		runtimeRef: "rust"
		package:    "tokei"
	}
}
