package tomei

import "tomei.terassyi.net/presets/aqua"

// Phase 2 demo: privileged: true routes the symlink to /usr/local/bin
// (default SystemBinDir) instead of ~/.local/bin. Requires `tomei apply --system`.
// Apple Silicon macOS uses /opt/homebrew by default; tomei currently routes
// privileged symlinks to /usr/local/bin regardless. Overriding requires a
// recompile via path.WithSystemBinDir — there is no end-user knob today.
privilegedTools: aqua.#AquaToolSet & {
	metadata: {
		name:        "privileged-tools"
		description: "Tools symlinked into the system bin directory under --system"
	}
	spec: tools: {
		lazygit: {package: "jesseduffield/lazygit", version: "v0.62.1", privileged: true}
	}
}
