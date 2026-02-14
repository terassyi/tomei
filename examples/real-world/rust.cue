package tomei

import "tomei.terassyi.net/presets/rust"

// cargo-binstall — installed via cargo install, then used as an Installer
cargoBinstall: rust.#CargoBinstall

// binstall Installer (delegation) — depends on cargo-binstall tool
binstallInstaller: rust.#BinstallInstaller
