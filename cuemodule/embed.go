// Package cuemodule provides embedded CUE schema and preset files for the
// tomei module (tomei.terassyi.net@v0). The directory doubles as a publishable
// CUE module: CI runs `cue mod publish` from this directory.
package cuemodule

import _ "embed"

//go:embed schema/schema.cue
var SchemaCUE string
