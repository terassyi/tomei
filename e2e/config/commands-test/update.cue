package tomei

// Mock tool for verifying commands.update vs commands.install dispatch.
// When --update-tools is used, the engine runs commands.update instead of commands.install.
// The marker file content distinguishes which command ran:
// "installed" = install command was used, "updated" = update command was used.
// Real tools (like mise) cannot distinguish this because their update has no observable side-effect file.

_updateMarkerDir: "/tmp/tomei-cmd-update-test"

updateTool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "update-tool"
	spec: {
		commands: {
			install:        ["mkdir -p \(_updateMarkerDir) && echo installed > \(_updateMarkerDir)/marker"]
			update:         ["echo updated > \(_updateMarkerDir)/marker"]
			check:          ["test -f \(_updateMarkerDir)/marker"]
			remove:         ["rm -rf \(_updateMarkerDir)"]
			resolveVersion: ["echo 2.0.0"]
		}
	}
}
