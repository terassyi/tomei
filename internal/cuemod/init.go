package cuemod

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/terassyi/tomei/internal/config"
)

//go:embed templates/module.cue.tmpl
var moduleTmpl string

//go:embed templates/platform.cue.tmpl
var platformTmpl string

const (
	DefaultModuleName = "manifests.local@v0"
	DefaultModuleVer  = "v0.0.1"
)

// ModuleParams holds the parameters for module.cue template rendering.
type ModuleParams struct {
	ModuleName      string
	LanguageVersion string
	TomeiModulePath string
	ModuleVersion   string
}

// GenerateModuleCUE generates the cue.mod/module.cue content from the embedded template.
func GenerateModuleCUE(moduleName string) ([]byte, error) {
	tmpl, err := template.New("module").Parse(moduleTmpl)
	if err != nil {
		return nil, fmt.Errorf("failed to parse module template: %w", err)
	}
	params := ModuleParams{
		ModuleName:      moduleName,
		LanguageVersion: config.CUELanguageVersion,
		TomeiModulePath: config.TomeiModulePath,
		ModuleVersion:   DefaultModuleVer,
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, params); err != nil {
		return nil, fmt.Errorf("failed to execute module template: %w", err)
	}
	return buf.Bytes(), nil
}

// GeneratePlatformCUE generates the tomei_platform.cue content from the embedded template.
func GeneratePlatformCUE() ([]byte, error) {
	tmpl, err := template.New("platform").Parse(platformTmpl)
	if err != nil {
		return nil, fmt.Errorf("failed to parse platform template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, nil); err != nil {
		return nil, fmt.Errorf("failed to execute platform template: %w", err)
	}
	return buf.Bytes(), nil
}

// WriteFileIfAllowed writes content to path, creating parent directories.
// Returns an error if the file exists and force is false.
func WriteFileIfAllowed(path string, content []byte, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists (use --force to overwrite)", path)
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", path, err)
	}

	if err := os.WriteFile(path, content, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}

	return nil
}

// RelativePath returns the relative path from base to target, or the absolute path on error.
func RelativePath(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return target
	}
	return rel
}
