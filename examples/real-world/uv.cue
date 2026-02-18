package tomei

import "tomei.terassyi.net/presets/python"

// Python tools installed via uv tool install
uvTools: python.#UvToolSet & {
	metadata: name: "uv-tools"
	spec: tools: {
		ruff: {package: "ruff", version: "0.15.1"}
		mypy: {package: "mypy", version: "1.19.1"}
		httpie: {package: "httpie", version: "3.2.4"}
		ansible: {package: "ansible", version: "13.3.0", args: ["--with-executables-from", "ansible-core"]}
	}
}
