package tomei

// Platform values resolved by tomei apply.
// For cue eval: cue eval -t os=linux -t arch=amd64
_os:       string @tag(os)
_arch:     string @tag(arch)
_headless: bool | *false @tag(headless,type=bool)
