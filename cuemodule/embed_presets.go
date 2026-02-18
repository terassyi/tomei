//go:build integration

package cuemodule

import "embed"

//go:embed presets/go/go.cue presets/rust/rust.cue presets/aqua/aqua.cue presets/node/node.cue presets/python/python.cue presets/deno/deno.cue presets/bun/bun.cue
var PresetsFS embed.FS
