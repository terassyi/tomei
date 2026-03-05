// Package aqua provides template rendering for aqua-registry asset names.
package aqua

import (
	"bytes"
	"strings"
	"text/template"
	"unicode"
	"unicode/utf8"
)

// TemplateVars holds variables for rendering asset name templates.
type TemplateVars struct {
	Version string // Package version (e.g., "v2.86.0")
	SemVer  string // Version with version_prefix stripped (e.g., "v2.86.0" when prefix is "kustomize/")
	OS      string // OS name after replacements (e.g., "darwin", "linux")
	Arch    string // Architecture after replacements (e.g., "amd64", "x86_64")
	Format  string // Archive format (e.g., "tar.gz", "zip")
	Asset   string // Rendered asset name (used for checksum templates like "{{.Asset}}.sha256")
}

// templateFuncs defines custom functions available in templates.
// These are compatible with aqua's Sprig-based template engine.
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

	// title uppercases the first character of a string.
	// Used by aqua-registry (e.g., goreleaser, porter): {{title .OS}} "linux" → "Linux".
	// Note: unlike Sprig's title (strings.Title), this only uppercases the first rune,
	// not each word. Sufficient for aqua-registry where inputs are single-word OS names.
	"title": func(s string) string {
		r, size := utf8.DecodeRuneInString(s)
		if r == utf8.RuneError {
			return s
		}
		return string(unicode.ToUpper(r)) + s[size:]
	},

	// tolower converts a string to lowercase.
	// Example: {{tolower .OS}} with "Darwin" returns "darwin"
	"tolower": strings.ToLower,

	// toupper converts a string to uppercase.
	// Example: {{toupper .OS}} with "linux" returns "LINUX"
	"toupper": strings.ToUpper,
}

// RenderTemplate renders a template string with the given variables.
// It supports custom functions: trimV, trimPrefix, trimSuffix, title, tolower, toupper.
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
