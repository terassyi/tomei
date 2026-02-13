package config

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/mod/modconfig"

	"github.com/terassyi/tomei/internal/config/schema"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/presets"
)

const (
	// ConfigFileName is the name of the tomei config file that should be ignored when loading manifests.
	ConfigFileName = "config.cue"

	// virtualModuleCUE is the contents of the virtual cue.mod/module.cue.
	virtualModuleCUE = `module: "tomei.terassyi.net@v0"
language: version: "v0.9.0"
`

	// DefaultCUERegistry is the built-in CUE_REGISTRY mapping for tomei modules.
	// When CUE_REGISTRY is not set, this default is used to resolve
	// tomei.terassyi.net imports from the OCI registry on ghcr.io.
	DefaultCUERegistry = "tomei.terassyi.net=ghcr.io/terassyi"
)

// Loader loads and parses CUE configuration files.
type Loader struct {
	ctx         *cue.Context
	env         *Env
	schemaValue cue.Value
}

// NewLoader creates a new Loader with the given environment.
func NewLoader(env *Env) *Loader {
	if env == nil {
		env = DetectEnv()
	}
	ctx := cuecontext.New()
	return &Loader{
		ctx:         ctx,
		env:         env,
		schemaValue: ctx.CompileString(schema.SchemaCUE),
	}
}

// buildRegistry creates a modconfig.Registry for CUE module resolution.
// It uses the CUE_REGISTRY environment variable if set, otherwise falls back
// to the built-in default (tomei.terassyi.net=ghcr.io/terassyi).
func buildRegistry() (modconfig.Registry, error) {
	cueRegistry := os.Getenv("CUE_REGISTRY")
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

// presetEnvCUE returns a CUE source string that defines _headless
// for a given preset package. This is injected into preset package directories via overlay
// because CUE's tag injection (Tags/TagVars) only applies to top-level loaded instances,
// not to imported packages.
//
// Note: _os and _arch are no longer injected here. Presets that need platform
// information (e.g., Go) now accept explicit platform parameters from the user manifest.
func (l *Loader) presetEnvCUE(pkgName string) string {
	return fmt.Sprintf("package %s\n\n_headless: %v\n",
		pkgName, l.env.Headless)
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

// buildOverlay creates the CUE overlay map with the virtual module structure for preset imports.
func (l *Loader) buildOverlay(absDir string) (map[string]load.Source, error) {
	overlay := make(map[string]load.Source)

	// Add virtual module overlay if no real cue.mod/ exists
	if !hasRealCueMod(absDir) {
		if err := l.buildVirtualModuleOverlay(absDir, overlay); err != nil {
			return nil, fmt.Errorf("failed to build virtual module overlay: %w", err)
		}
	}

	return overlay, nil
}

// buildVirtualModuleOverlay adds cue.mod/module.cue and preset packages to the overlay.
// This enables CUE import statements like: import "tomei.terassyi.net/presets/aqua"
//
// Presets are placed as intra-module packages under <absDir>/presets/<pkg>/ because
// the module is declared as "tomei.terassyi.net@v0", so CUE resolves imports starting
// with "tomei.terassyi.net/" relative to the module root directory.
func (l *Loader) buildVirtualModuleOverlay(absDir string, overlay map[string]load.Source) error {
	// Add cue.mod/module.cue
	moduleCuePath := filepath.Join(absDir, "cue.mod", "module.cue")
	overlay[moduleCuePath] = load.FromString(virtualModuleCUE)

	// Walk embedded preset files and place them as intra-module packages.
	// Track package names per directory for env injection.
	presetPkgs := make(map[string]string) // dir -> package name
	err := fs.WalkDir(presets.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".cue" {
			return nil
		}

		data, err := presets.FS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read embedded preset %s: %w", path, err)
		}

		// Place at <absDir>/presets/<pkg>/<file>.cue (intra-module path)
		overlayPath := filepath.Join(absDir, "presets", path)
		overlay[overlayPath] = load.FromString(string(data))

		// Track package name for env injection
		dir := filepath.Dir(path)
		if _, ok := presetPkgs[dir]; !ok {
			if pkg := detectPackageName(string(data)); pkg != "" {
				presetPkgs[dir] = pkg
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk preset files: %w", err)
	}

	// Inject tomei_env_gen.cue into each preset package directory.
	// CUE's tag injection (Tags/TagVars) only applies to top-level loaded instances,
	// so imported preset packages need concrete values injected via overlay.
	// Note: filename must NOT start with "_" or "." as CUE ignores such files.
	for dir, pkg := range presetPkgs {
		envPath := filepath.Join(absDir, "presets", dir, "tomei_env_gen.cue")
		overlay[envPath] = load.FromString(l.presetEnvCUE(pkg))
	}

	// Add schema package for import "tomei.terassyi.net/schema"
	schemaPath := filepath.Join(absDir, "schema", "schema.cue")
	overlay[schemaPath] = load.FromString(schema.SchemaCUE)

	return nil
}

// hasRealCueMod checks whether a real cue.mod/ directory exists at or above dir.
// If found, the virtual module overlay is skipped to respect user-managed modules.
func hasRealCueMod(dir string) bool {
	cur := dir
	for {
		if info, err := os.Stat(filepath.Join(cur, "cue.mod")); err == nil && info.IsDir() {
			return true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return false
}

// Load loads CUE configuration from the given directory.
// config.cue files are excluded from loading as they contain tomei configuration, not manifests.
func (l *Loader) Load(dir string) ([]resource.Resource, error) {
	// Check if directory exists
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to access config directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}

	// Collect all .cue files except config.cue
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
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
		return nil, nil
	}

	// Convert dir to absolute path for overlay (CUE requires absolute paths)
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Read file sources to detect @tag() declarations
	var sources []string
	for _, f := range cueFiles {
		data, err := os.ReadFile(filepath.Join(absDir, f))
		if err != nil {
			return nil, fmt.Errorf("failed to read file %s: %w", f, err)
		}
		sources = append(sources, string(data))
	}

	// Build load configuration: use registry when cue.mod/ exists and registry is set,
	// otherwise use virtual module overlay.
	loadCfg := &load.Config{
		Dir:  absDir,
		Tags: l.envTagsForSources(sources...),
	}

	if hasRealCueMod(absDir) {
		// OCI registry path: build a registry and let CUE resolve imports.
		registry, err := buildRegistry()
		if err != nil {
			return nil, fmt.Errorf("failed to build CUE registry: %w", err)
		}
		loadCfg.Registry = registry
	} else {
		// Virtual overlay path: build in-memory module structure.
		overlay, err := l.buildOverlay(absDir)
		if err != nil {
			return nil, err
		}
		loadCfg.Overlay = overlay
	}

	// Load CUE files with configured loader
	instances := load.Instances(cueFiles, loadCfg)

	if len(instances) == 0 {
		return nil, fmt.Errorf("no CUE files found in %s", dir)
	}

	inst := instances[0]
	if inst.Err != nil {
		return nil, fmt.Errorf("failed to load CUE files: %w", inst.Err)
	}

	// Build the value
	value := l.ctx.BuildInstance(inst)
	if value.Err() != nil {
		return nil, fmt.Errorf("failed to build CUE value: %w", value.Err())
	}

	return l.parseResources(value)
}

// LoadPaths loads resources from multiple files or directories.
func (l *Loader) LoadPaths(paths []string) ([]resource.Resource, error) {
	var allResources []resource.Resource

	for _, p := range paths {
		// Expand ~ to home directory
		var expanded string
		switch {
		case strings.HasPrefix(p, "~/"):
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("failed to expand path %s: %w", p, err)
			}
			expanded = filepath.Join(home, p[2:])
		case p == "~":
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("failed to expand path %s: %w", p, err)
			}
			expanded = home
		default:
			expanded = p
		}

		info, err := os.Stat(expanded)
		if err != nil {
			return nil, fmt.Errorf("failed to access %s: %w", expanded, err)
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
	// Skip config.cue file
	if filepath.Base(path) == ConfigFileName {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	source := string(data)
	pkgName := detectPackageName(source)

	// Files with a package declaration: use load.Instances() for import and @tag() resolution
	if pkgName != "" {
		return l.loadFileWithInstancesFromSource(path, source)
	}

	// No package declaration: use CompileString() (imports and @tag() not available)
	value := l.ctx.CompileString(source, cue.Filename(path))
	if value.Err() != nil {
		return nil, fmt.Errorf("failed to compile CUE: %w", value.Err())
	}

	return l.parseResources(value)
}

// loadFileWithInstancesFromSource loads a single CUE file using load.Instances() to support imports.
// The source parameter contains the already-read file content.
func (l *Loader) loadFileWithInstancesFromSource(path, source string) ([]resource.Resource, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}
	absDir := filepath.Dir(absPath)
	fileName := filepath.Base(absPath)

	files := []string{fileName}

	loadCfg := &load.Config{
		Dir:  absDir,
		Tags: l.envTagsForSources(source),
	}

	if hasRealCueMod(absDir) {
		registry, err := buildRegistry()
		if err != nil {
			return nil, fmt.Errorf("failed to build CUE registry: %w", err)
		}
		loadCfg.Registry = registry
	} else {
		overlay, err := l.buildOverlay(absDir)
		if err != nil {
			return nil, err
		}
		loadCfg.Overlay = overlay
	}

	instances := load.Instances(files, loadCfg)

	if len(instances) == 0 {
		return nil, fmt.Errorf("no CUE instances loaded for %s", path)
	}

	inst := instances[0]
	if inst.Err != nil {
		return nil, fmt.Errorf("failed to load CUE file: %w", inst.Err)
	}

	value := l.ctx.BuildInstance(inst)
	if value.Err() != nil {
		return nil, fmt.Errorf("failed to build CUE value: %w", value.Err())
	}

	return l.parseResources(value)
}

// validateResource validates a resource value against the #Resource schema definition
// using the internally compiled schema.
func (l *Loader) validateResource(field cue.Value, name string) (cue.Value, error) {
	resourceDef := l.schemaValue.LookupPath(cue.ParsePath("#Resource"))
	if !resourceDef.Exists() {
		return field, nil
	}
	unified := field.Unify(resourceDef)
	if err := unified.Validate(cue.Concrete(true)); err != nil {
		return field, fmt.Errorf("schema validation failed for %q: %w", name, err)
	}
	return unified, nil
}

func (l *Loader) parseResources(value cue.Value) ([]resource.Resource, error) {
	var resources []resource.Resource

	// Check if value is a list (multiple resources)
	if iter, err := value.List(); err == nil {
		for iter.Next() {
			validated, err := l.validateResource(iter.Value(), "")
			if err != nil {
				return nil, err
			}
			res, err := l.parseResource(validated)
			if err != nil {
				return nil, err
			}
			resources = append(resources, res)
		}
		return resources, nil
	}

	// Check if it has apiVersion (single resource at top level)
	if value.LookupPath(cue.ParsePath("apiVersion")).Exists() {
		validated, err := l.validateResource(value, "")
		if err != nil {
			return nil, err
		}
		res, err := l.parseResource(validated)
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
		validated, err := l.validateResource(fieldValue, iter.Selector().String())
		if err != nil {
			return nil, err
		}
		res, err := l.parseResource(validated)
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
	jsonBytes, err := value.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal resource to JSON: %w", err)
	}

	// Extract kind to determine the concrete type
	var base resource.BaseResource
	if err := json.Unmarshal(jsonBytes, &base); err != nil {
		return nil, fmt.Errorf("failed to unmarshal BaseResource: %w", err)
	}

	switch base.ResourceKind {
	case resource.KindTool:
		return unmarshalResource[*resource.Tool](jsonBytes)
	case resource.KindToolSet:
		return unmarshalResource[*resource.ToolSet](jsonBytes)
	case resource.KindRuntime:
		return unmarshalResource[*resource.Runtime](jsonBytes)
	case resource.KindInstaller:
		return unmarshalResource[*resource.Installer](jsonBytes)
	case resource.KindInstallerRepository:
		return unmarshalResource[*resource.InstallerRepository](jsonBytes)
	case resource.KindSystemInstaller:
		return unmarshalResource[*resource.SystemInstaller](jsonBytes)
	case resource.KindSystemPackageRepository:
		return unmarshalResource[*resource.SystemPackageRepository](jsonBytes)
	case resource.KindSystemPackageSet:
		return unmarshalResource[*resource.SystemPackageSet](jsonBytes)
	default:
		return nil, fmt.Errorf("unknown kind: %s", base.ResourceKind)
	}
}

// unmarshalResource unmarshals JSON bytes into a concrete resource type.
func unmarshalResource[R resource.Resource](jsonBytes []byte) (R, error) {
	var res R
	if err := json.Unmarshal(jsonBytes, &res); err != nil {
		return res, err
	}
	return res, nil
}
