package tomei

// Rust ecosystem: rustup delegation → cargo-binstall → tools via binstall
//
// Dependency chain:
//   Runtime/rust → Tool/cargo-binstall (cargo install)
//                → Installer/binstall (delegation, toolRef: cargo-binstall)
//                    → ToolSet/rust-tools (installerRef: binstall)
//                → Tool/tokei (cargo install, runtimeRef: rust)

// Rust Runtime (delegation via rustup)
rustRuntime: #RustRuntime & {
	spec: version: "stable"
}

// cargo-binstall — installed via cargo install, then used as an Installer
cargoBinstall: #CargoBinstall

// binstall Installer (delegation) — depends on cargo-binstall tool
binstallInstaller: #BinstallInstaller

// ToolSet: Rust tools installed via binstall (fast pre-built binary downloads)
rustTools: #BinstallToolSet & {
	metadata: {
		name:        "rust-tools"
		description: "Rust CLI tools installed via cargo-binstall"
	}
	spec: tools: {
		eza:       {package: "eza"}
		hyperfine: {package: "hyperfine"}
		ripgrep:   {package: "ripgrep"}
	}
}

// Tool installed via cargo install (source compilation, runtimeRef)
// cargo-binstall does not support tokei, so we fall back to cargo install
tokei: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: {
		name:        "tokei"
		description: "Count lines of code (compiled from source via cargo install)"
	}
	spec: {
		runtimeRef: "rust"
		package:    "tokei"
	}
}
