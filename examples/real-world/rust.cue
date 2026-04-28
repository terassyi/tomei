package tomei

import "tomei.terassyi.net/presets/rust"

// cargo-binstall — installed via cargo install, then used as an Installer
cargoBinstall: rust.#CargoBinstall

// binstall Installer (delegation) — depends on cargo-binstall tool
binstallInstaller: rust.#BinstallInstaller

// rustup-component Installer — delegates to `rustup component add/remove`.
// Operates on the rustup-active toolchain (default = the one rustRuntime set up).
rustupComponentInstaller: rust.#RustupComponentInstaller

// rustup-managed components (rust-analyzer, etc.). Map keys double as
// the rustup component name.
rustComponents: rust.#RustupComponentToolSet & {
	metadata: name: "rust-components"
	spec: tools: {
		"rust-analyzer": {}
		"rust-src":      {}
	}
}
