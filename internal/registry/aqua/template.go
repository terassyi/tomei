// Package aqua provides template rendering for aqua-registry asset names.
package aqua

import (
	"bytes"
	"strings"
	"text/template"
)

// TemplateVars holds variables for rendering asset name templates.
type TemplateVars struct {
	Version string // Version with "v" prefix (e.g., "v2.86.0")
	SemVer  string // Version without "v" prefix (e.g., "2.86.0")
	OS      string // OS name after replacements (e.g., "darwin", "linux")
	Arch    string // Architecture after replacements (e.g., "amd64", "x86_64")
	Format  string // Archive format (e.g., "tar.gz", "zip")
	Asset   string // Rendered asset name (used for checksum templates like "{{.Asset}}.sha256")
}

// templateFuncs defines custom functions available in templates.
var templateFuncs = template.FuncMap{
	// trimV removes the "v" prefix from a version string.
	// Example: {{trimV .Version}} with "v2.86.0" returns "2.86.0"
	"trimV": func(v string) string {
		return strings.TrimPrefix(v, "v")
	},

	// trimPrefix removes a prefix from a string.
	// Example: {{trimPrefix .OS "darwin"}} with "darwin" returns ""
	"trimPrefix": strings.TrimPrefix,

	// trimSuffix removes a suffix from a string.
	// Example: {{trimSuffix .Format ".gz"}} with "tar.gz" returns "tar"
	"trimSuffix": strings.TrimSuffix,
}

// RenderTemplate renders a template string with the given variables.
// It supports custom functions: trimV, trimPrefix, trimSuffix.
func RenderTemplate(tmpl string, vars TemplateVars) (string, error) {
	t, err := template.New("asset").Funcs(templateFuncs).Parse(tmpl)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, vars); err != nil {
		return "", err
	}

	return buf.String(), nil
}
