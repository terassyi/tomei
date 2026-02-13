//go:build integration

package cuemodule

import "embed"

//go:embed presets/go/go.cue presets/rust/rust.cue presets/aqua/aqua.cue
var PresetsFS embed.FS
