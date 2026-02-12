package presets

import "embed"

//go:embed go/go.cue rust/rust.cue aqua/aqua.cue
var FS embed.FS
