package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/mod/modconfig"

	"github.com/terassyi/tomei/internal/resource"
)

const (
	// ConfigFileName is the name of the tomei config file that should be ignored when loading manifests.
	ConfigFileName = "config.cue"

	// TomeiModulePath is the CUE module path for the tomei module.
	TomeiModulePath = "tomei.terassyi.net@v0"

	// CUELanguageVersion is the CUE language version used by the tomei module.
	CUELanguageVersion = "v0.9.0"

	// EnvCUERegistry is the environment variable name for the CUE registry.
	EnvCUERegistry = "CUE_REGISTRY"

	// DefaultCUERegistry is the built-in CUE_REGISTRY mapping for tomei modules.
	// When CUE_REGISTRY is not set, this default is used to resolve
	// tomei.terassyi.net imports from the OCI registry on ghcr.io.
	DefaultCUERegistry = "tomei.terassyi.net=ghcr.io/terassyi"
)

// Loader loads and parses CUE configuration files.
type Loader struct {
	ctx *cue.Context
	env *Env
}

// NewLoader creates a new Loader with the given environment.
func NewLoader(env *Env) *Loader {
	if env == nil {
		env = DetectEnv()
	}
	return &Loader{
		ctx: cuecontext.New(),
		env: env,
	}
}

// buildRegistry creates a modconfig.Registry for CUE module resolution.
// It uses the CUE_REGISTRY environment variable if set, otherwise falls back
// to the built-in default (tomei.terassyi.net=ghcr.io/terassyi).
func buildRegistry() (modconfig.Registry, error) {
	cueRegistry := os.Getenv(EnvCUERegistry)
	if cueRegistry == "" {
		cueRegistry = DefaultCUERegistry
	}
	return modconfig.NewRegistry(&modconfig.Config{
		CUERegistry: cueRegistry,
	})
}

// envTagsForSources scans CUE source texts for @tag() declarations via AST and returns
// only the Tags entries that have matching declarations. This avoids the CUE loader
// error "no tag for X" that occurs when Tags contains entries without corresponding
// @tag() declarations in the loaded files.
func (l *Loader) envTagsForSources(sources ...string) []string {
	declared := scanDeclaredTags(sources...)
	var tags []string
	if declared["os"] {
		tags = append(tags, "os="+string(l.env.OS))
	}
	if declared["arch"] {
		tags = append(tags, "arch="+string(l.env.Arch))
	}
	if declared["headless"] {
		tags = append(tags, fmt.Sprintf("headless=%t", l.env.Headless))
	}
	return tags
}

// scanDeclaredTags parses CUE sources and returns the set of tag names
// declared via @tag() attributes on fields.
func scanDeclaredTags(sources ...string) map[string]bool {
	tags := make(map[string]bool)
	for _, src := range sources {
		f, err := parser.ParseFile("", src)
		if err != nil {
			continue
		}
		ast.Walk(f, func(n ast.Node) bool {
			field, ok := n.(*ast.Field)
			if !ok {
				return true
			}
			for _, a := range field.Attrs {
				key, body := a.Split()
				if key == "tag" {
					name, _, _ := strings.Cut(body, ",")
					tags[name] = true
				}
			}
			return true
		}, nil)
	}
	return tags
}

// detectPackageName extracts the package name from CUE source code.
// Returns empty string if no package declaration is found.
func detectPackageName(source string) string {
	for line := range strings.SplitSeq(source, "\n") {
		line = strings.TrimSpace(line)
		if pkg, found := strings.CutPrefix(line, "package "); found {
			return pkg
		}
		// Skip empty lines and comments at the beginning
		if line != "" && !strings.HasPrefix(line, "//") {
			break
		}
	}
	return ""
}

// buildLoadConfig creates a load.Config with CUE module registry for import resolution.
// A cue.mod/ directory is expected to exist at or above absDir (created by `tomei cue init`).
func (l *Loader) buildLoadConfig(absDir string, tags []string) (*load.Config, error) {
	registry, err := buildRegistry()
	if err != nil {
		return nil, fmt.Errorf("failed to build CUE registry: %w", err)
	}
	return &load.Config{
		Dir:      absDir,
		Tags:     tags,
		Registry: registry,
	}, nil
}

// Load loads CUE configuration from the given directory.
// config.cue files are excluded from loading as they contain tomei configuration, not manifests.
func (l *Loader) Load(dir string) ([]resource.Resource, error) {
	value, err := l.evalDir(dir)
	if err != nil {
		return nil, err
	}
	// evalDir returns zero value when no CUE files found
	if !value.Exists() {
		return nil, nil
	}
	return l.parseResources(value)
}

// EvalDir evaluates CUE files in a directory and returns the unified cue.Value
// without parsing into resource types. Used by tomei cue eval/export.
func (l *Loader) EvalDir(dir string) (cue.Value, error) {
	return l.evalDir(dir)
}

// evalDir is the internal implementation that builds a cue.Value from a directory.
func (l *Loader) evalDir(dir string) (cue.Value, error) {
	var zero cue.Value

	// Check if directory exists
	info, err := os.Stat(dir)
	if err != nil {
		return zero, fmt.Errorf("failed to access config directory: %w", err)
	}
	if !info.IsDir() {
		return zero, fmt.Errorf("%s is not a directory", dir)
	}

	// Collect all .cue files except config.cue
	entries, err := os.ReadDir(dir)
	if err != nil {
		return zero, fmt.Errorf("failed to read directory: %w", err)
	}

	var cueFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) == ".cue" && name != ConfigFileName {
			cueFiles = append(cueFiles, name)
		}
	}

	if len(cueFiles) == 0 {
		return zero, nil
	}

	// Convert dir to absolute path for overlay (CUE requires absolute paths)
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return zero, fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Read file sources to detect @tag() declarations
	var sources []string
	for _, f := range cueFiles {
		data, err := os.ReadFile(filepath.Join(absDir, f))
		if err != nil {
			return zero, fmt.Errorf("failed to read file %s: %w", f, err)
		}
		sources = append(sources, string(data))
	}

	// Build load configuration with CUE module registry.
	loadCfg, err := l.buildLoadConfig(absDir, l.envTagsForSources(sources...))
	if err != nil {
		return zero, err
	}

	// Load CUE files with configured loader
	instances := load.Instances(cueFiles, loadCfg)

	if len(instances) == 0 {
		return zero, fmt.Errorf("no CUE files found in %s", dir)
	}

	inst := instances[0]
	if inst.Err != nil {
		return zero, fmt.Errorf("failed to load CUE files: %w", inst.Err)
	}

	// Build the value
	value := l.ctx.BuildInstance(inst)
	if value.Err() != nil {
		return zero, fmt.Errorf("failed to build CUE value: %w", value.Err())
	}

	return value, nil
}

// expandHome expands a leading ~ to the user's home directory.
func expandHome(p string) (string, error) {
	switch {
	case strings.HasPrefix(p, "~/"):
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to expand path %s: %w", p, err)
		}
		return filepath.Join(home, p[2:]), nil
	case p == "~":
		return os.UserHomeDir()
	default:
		return p, nil
	}
}

// expandAndStat expands ~ in a path and returns the expanded path with its FileInfo.
func expandAndStat(p string) (string, os.FileInfo, error) {
	expanded, err := expandHome(p)
	if err != nil {
		return "", nil, fmt.Errorf("failed to expand path %s: %w", p, err)
	}

	info, err := os.Stat(expanded)
	if err != nil {
		return "", nil, fmt.Errorf("failed to access %s: %w", expanded, err)
	}

	return expanded, info, nil
}

// LoadPaths loads resources from multiple files or directories.
func (l *Loader) LoadPaths(paths []string) ([]resource.Resource, error) {
	var allResources []resource.Resource

	for _, p := range paths {
		expanded, info, err := expandAndStat(p)
		if err != nil {
			return nil, err
		}

		var resources []resource.Resource
		if info.IsDir() {
			resources, err = l.Load(expanded)
		} else {
			resources, err = l.LoadFile(expanded)
		}
		if err != nil {
			return nil, err
		}
		allResources = append(allResources, resources...)
	}

	return allResources, nil
}

// LoadFile loads a single CUE file.
// If the file is config.cue, it is skipped and returns empty resources.
// Files with a package declaration use load.Instances() so that import statements are resolved.
// Files without a package declaration use CompileString() (import is not available without a package).
func (l *Loader) LoadFile(path string) ([]resource.Resource, error) {
	value, err := l.evalFile(path)
	if err != nil {
		return nil, err
	}
	// evalFile returns zero value for config.cue
	if !value.Exists() {
		return nil, nil
	}
	return l.parseResources(value)
}

// EvalFile evaluates a single CUE file and returns the cue.Value
// without parsing into resource types. Used by tomei cue eval/export.
func (l *Loader) EvalFile(path string) (cue.Value, error) {
	return l.evalFile(path)
}

// evalFile is the internal implementation that builds a cue.Value from a single file.
func (l *Loader) evalFile(path string) (cue.Value, error) {
	var zero cue.Value

	// Skip config.cue file
	if filepath.Base(path) == ConfigFileName {
		return zero, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return zero, fmt.Errorf("failed to read file: %w", err)
	}

	source := string(data)
	pkgName := detectPackageName(source)

	// Files with a package declaration: use load.Instances() for import and @tag() resolution
	if pkgName != "" {
		return l.evalFileWithInstances(path, source)
	}

	// No package declaration: use CompileString() (imports and @tag() not available)
	value := l.ctx.CompileString(source, cue.Filename(path))
	if value.Err() != nil {
		return zero, fmt.Errorf("failed to compile CUE: %w", value.Err())
	}

	return value, nil
}

// evalFileWithInstances loads a single CUE file using load.Instances() to support imports.
// The source parameter contains the already-read file content.
func (l *Loader) evalFileWithInstances(path, source string) (cue.Value, error) {
	var zero cue.Value

	absPath, err := filepath.Abs(path)
	if err != nil {
		return zero, fmt.Errorf("failed to get absolute path: %w", err)
	}
	absDir := filepath.Dir(absPath)
	fileName := filepath.Base(absPath)

	files := []string{fileName}

	loadCfg, err := l.buildLoadConfig(absDir, l.envTagsForSources(source))
	if err != nil {
		return zero, err
	}

	instances := load.Instances(files, loadCfg)

	if len(instances) == 0 {
		return zero, fmt.Errorf("no CUE instances loaded for %s", path)
	}

	inst := instances[0]
	if inst.Err != nil {
		return zero, fmt.Errorf("failed to load CUE file: %w", inst.Err)
	}

	value := l.ctx.BuildInstance(inst)
	if value.Err() != nil {
		return zero, fmt.Errorf("failed to build CUE value: %w", value.Err())
	}

	return value, nil
}

// EvalPaths evaluates multiple files or directories and returns a slice of cue.Values.
// Each path produces one cue.Value. Used by tomei cue eval/export.
func (l *Loader) EvalPaths(paths []string) ([]cue.Value, error) {
	var values []cue.Value

	for _, p := range paths {
		expanded, info, err := expandAndStat(p)
		if err != nil {
			return nil, err
		}

		var value cue.Value
		if info.IsDir() {
			value, err = l.evalDir(expanded)
		} else {
			value, err = l.evalFile(expanded)
		}
		if err != nil {
			return nil, err
		}
		if value.Exists() {
			values = append(values, value)
		}
	}

	return values, nil
}

func (l *Loader) parseResources(value cue.Value) ([]resource.Resource, error) {
	var resources []resource.Resource

	// Check if value is a list (multiple resources)
	if iter, err := value.List(); err == nil {
		for iter.Next() {
			res, err := l.parseResource(iter.Value())
			if err != nil {
				return nil, err
			}
			resources = append(resources, res)
		}
		return resources, nil
	}

	// Check if it has apiVersion (single resource at top level)
	if value.LookupPath(cue.ParsePath("apiVersion")).Exists() {
		res, err := l.parseResource(value)
		if err != nil {
			return nil, err
		}
		return []resource.Resource{res}, nil
	}

	// Otherwise, iterate over struct fields to find resources
	iter, err := value.Fields(cue.Definitions(false), cue.Hidden(false))
	if err != nil {
		return nil, fmt.Errorf("failed to iterate fields: %w", err)
	}

	for iter.Next() {
		fieldValue := iter.Value()
		// Skip if not a resource (no apiVersion)
		if !fieldValue.LookupPath(cue.ParsePath("apiVersion")).Exists() {
			continue
		}
		res, err := l.parseResource(fieldValue)
		if err != nil {
			return nil, err
		}
		resources = append(resources, res)
	}

	if len(resources) == 0 {
		return nil, fmt.Errorf("no resources found")
	}

	return resources, nil
}

func (l *Loader) parseResource(value cue.Value) (resource.Resource, error) {
	// Extract kind to determine the concrete type
	var base resource.BaseResource
	if err := value.Decode(&base); err != nil {
		return nil, fmt.Errorf("failed to decode BaseResource: %w", err)
	}

	switch base.ResourceKind {
	case resource.KindTool:
		return decodeResource[*resource.Tool](value)
	case resource.KindToolSet:
		return decodeResource[*resource.ToolSet](value)
	case resource.KindRuntime:
		return decodeResource[*resource.Runtime](value)
	case resource.KindInstaller:
		return decodeResource[*resource.Installer](value)
	case resource.KindInstallerRepository:
		return decodeResource[*resource.InstallerRepository](value)
	case resource.KindSystemInstaller:
		return decodeResource[*resource.SystemInstaller](value)
	case resource.KindSystemPackageRepository:
		return decodeResource[*resource.SystemPackageRepository](value)
	case resource.KindSystemPackageSet:
		return decodeResource[*resource.SystemPackageSet](value)
	default:
		return nil, fmt.Errorf("unknown kind: %s", base.ResourceKind)
	}
}

// decodeResource decodes a CUE value directly into a concrete resource type.
// Custom UnmarshalJSON methods on resource types handle CUE's quirk of
// serializing single-element [...string] lists as bare strings.
func decodeResource[R resource.Resource](value cue.Value) (R, error) {
	var res R
	if err := value.Decode(&res); err != nil {
		return res, err
	}
	return res, nil
}
