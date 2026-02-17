package cuemod

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"
)

//go:embed templates/scaffold_*.cue.tmpl
var scaffoldTemplates embed.FS

// ScaffoldParams holds parameters for scaffold template rendering.
type ScaffoldParams struct {
	Bare bool
}

// supportedKinds maps user-facing kind names to template file names.
var supportedKinds = map[string]string{
	"tool":                 "scaffold_tool.cue.tmpl",
	"runtime":              "scaffold_runtime.cue.tmpl",
	"installer":            "scaffold_installer.cue.tmpl",
	"installer-repository": "scaffold_installer_repository.cue.tmpl",
	"toolset":              "scaffold_toolset.cue.tmpl",
}

// SupportedScaffoldKinds returns the list of supported kind names for scaffold.
func SupportedScaffoldKinds() []string {
	return []string{"tool", "runtime", "installer", "installer-repository", "toolset"}
}

// Scaffold generates a CUE manifest scaffold for the given resource kind.
func Scaffold(kind string, params ScaffoldParams) ([]byte, error) {
	tmplFile, ok := supportedKinds[kind]
	if !ok {
		return nil, fmt.Errorf("unsupported kind %q, supported kinds: tool, runtime, installer, installer-repository, toolset", kind)
	}

	data, err := scaffoldTemplates.ReadFile("templates/" + tmplFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read scaffold template for %s: %w", kind, err)
	}

	tmpl, err := template.New(kind).Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("failed to parse scaffold template for %s: %w", kind, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, params); err != nil {
		return nil, fmt.Errorf("failed to execute scaffold template for %s: %w", kind, err)
	}

	return buf.Bytes(), nil
}
