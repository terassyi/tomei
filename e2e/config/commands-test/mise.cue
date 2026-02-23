package tomei

// mise - a real-world commands-pattern tool (polyglot tool version manager).
// Installed via curl | sh, self-updates via `mise self-update`, removed via `mise implode`.
// This tests the full commands pattern lifecycle with an actual networked install.

mise: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "mise"
	spec: {
		commands: {
			install:        ["MISE_QUIET=1 curl -fsSL https://mise.jdx.dev/install.sh | sh"]
			check:          ["$HOME/.local/bin/mise --version"]
			remove:         ["$HOME/.local/bin/mise implode --yes"]
			resolveVersion: ["$HOME/.local/bin/mise --version 2>/dev/null | awk '{print $1}'"]
		}
	}
}
