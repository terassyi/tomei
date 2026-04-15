package tomei

// Privileged tool: should be skipped without --system flag.
// Uses a simple marker file to verify install behavior.
_markerDir: "/tmp/tomei-privileged-test"

privilegedTool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "privileged-tool"
	spec: {
		privileged: true
		commands: {
			install: ["mkdir -p \(_markerDir) && echo installed > \(_markerDir)/marker"]
			check: ["test -f \(_markerDir)/marker"]
			remove: ["rm -rf \(_markerDir)"]
		}
	}
}

// Non-privileged tool in the same manifest: should always be processed.
normalTool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "normal-tool"
	spec: {
		commands: {
			install:        ["mkdir -p /tmp/tomei-normal-test && echo installed > /tmp/tomei-normal-test/marker"]
			check:          ["test -f /tmp/tomei-normal-test/marker"]
			remove:         ["rm -rf /tmp/tomei-normal-test"]
			resolveVersion: ["echo 1.0.0"]
		}
	}
}
