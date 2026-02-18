package tomei

import (
	gopreset "tomei.terassyi.net/presets/go"
	"tomei.terassyi.net/presets/rust"
	"tomei.terassyi.net/presets/python"
	"tomei.terassyi.net/presets/node"
	"tomei.terassyi.net/presets/deno"
	"tomei.terassyi.net/presets/bun"
)

// Go runtime (download from go.dev)
goRuntime: gopreset.#GoRuntime & {
	platform: {os: _os, arch: _arch}
	spec: version: "1.26.0"
}

// Rust runtime (delegation via rustup)
rustRuntime: rust.#RustRuntime & {
	spec: version: "stable"
}

// uv runtime (delegation — standalone installer)
uvRuntime: python.#UvRuntime & {
	spec: version: "0.10.2"
}

// Node.js runtime (delegation via pnpm standalone installer)
pnpmRuntime: node.#PnpmRuntime & {
	spec: version: "10.29.3"
}

// Deno runtime (download from dl.deno.land)
denoRuntime: deno.#DenoRuntime & {
	platform: {os: _os, arch: _arch}
	spec: version: "2.1.4"
}

// Bun runtime (download from GitHub releases)
bunRuntime: bun.#BunRuntime & {
	platform: {os: _os, arch: _arch}
	spec: version: "1.1.42"
}
