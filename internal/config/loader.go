package config

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"

	"github.com/terassyi/tomei/internal/config/schema"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/presets"
)

const (
	// ConfigFileName is the name of the tomei config file that should be ignored when loading manifests.
	ConfigFileName = "config.cue"
	// envOverlayFileName is the virtual file name for the injected _env overlay.
	envOverlayFileName = "_env.cue"
	// schemaOverlayFileName is the virtual file name for the injected _schema overlay.
	schemaOverlayFileName = "_schema.cue"

	// virtualModuleCUE is the contents of the virtual cue.mod/module.cue.
	virtualModuleCUE = `module: "tomei.terassyi.net@v0"
language: version: "v0.9.0"
`
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

// Platform name mappings for different conventions
var (
	osAppleMap = map[OS]string{
		OSLinux:  "Linux",
		OSDarwin: "macOS",
	}
	archGnuMap = map[Arch]string{
		ArchAMD64: "x86_64",
		ArchARM64: "aarch64",
	}
)

// envCUE returns CUE source code that defines the _env hidden field.
// This is prepended to user CUE files to enable environment-specific configuration.
//
// Example usage in CUE:
//
//	// Basic (Go naming convention)
//	url: "https://go.dev/dl/go1.25.5.\(_env.os)-\(_env.arch).tar.gz"
//
//	// Apple naming convention (macOS, Linux)
//	url: "https://.../gh_\(_env.platform.os.apple)_\(_env.arch).tar.gz"
//
//	// GNU naming convention (x86_64, aarch64)
//	url: "https://.../ripgrep-\(_env.platform.arch.gnu)-apple-\(_env.os).tar.gz"
func (l *Loader) envCUE() string {
	return fmt.Sprintf(`_env: {
	os: %q
	arch: %q
	headless: %t
	platform: {
		os: {
			go: %q
			apple: %q
		}
		arch: {
			go: %q
			gnu: %q
		}
	}
}`,
		l.env.OS, l.env.Arch, l.env.Headless,
		l.env.OS, osAppleMap[l.env.OS],
		l.env.Arch, archGnuMap[l.env.Arch],
	)
}

// envCUEWithPackage returns CUE source code with package declaration.
// Used when loading directories where package declaration is required.
func (l *Loader) envCUEWithPackage(pkg string) string {
	return fmt.Sprintf("package %s\n%s", pkg, l.envCUE())
}

// schemaCUEAfterPackage returns the schema content with the "package tomei" line stripped.
// Used when injecting schema into files without a package declaration (LoadFile mode).
func schemaCUEAfterPackage() string {
	_, after, _ := strings.Cut(schema.SchemaCUE, "\n")
	return after
}

// schemaCUEWithPackage returns the schema content with a custom package declaration.
// Used when injecting schema into directories with a specific package name.
func schemaCUEWithPackage(pkg string) string {
	return fmt.Sprintf("package %s\n%s", pkg, schemaCUEAfterPackage())
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

// buildOverlay creates the CUE overlay map with _env.cue, _schema.cue, and optionally
// the virtual module structure for preset imports.
func (l *Loader) buildOverlay(absDir, pkgName string) (map[string]load.Source, error) {
	overlay := make(map[string]load.Source)

	// Add _env.cue and _schema.cue
	envFilePath := filepath.Join(absDir, envOverlayFileName)
	schemaFilePath := filepath.Join(absDir, schemaOverlayFileName)
	if pkgName != "" {
		overlay[envFilePath] = load.FromString(l.envCUEWithPackage(pkgName))
		overlay[schemaFilePath] = load.FromString(schemaCUEWithPackage(pkgName))
	} else {
		overlay[envFilePath] = load.FromString(l.envCUE())
		overlay[schemaFilePath] = load.FromString(schemaCUEAfterPackage())
	}

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
	// _env is prepended directly into the preset file content because CUE's loader
	// ignores files starting with '_' during package discovery.
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

		// Inject _env after the package declaration so preset CUE can reference _env.os etc.
		content := string(data)
		pkgName := detectPackageName(content)
		if pkgName != "" {
			pkgDecl := "package " + pkgName
			idx := strings.Index(content, pkgDecl)
			if idx >= 0 {
				afterPkg := idx + len(pkgDecl)
				content = content[:afterPkg] + "\n" + l.envCUE() + "\n" + content[afterPkg:]
			}
		}

		// Place at <absDir>/presets/<pkg>/<file>.cue (intra-module path)
		overlayPath := filepath.Join(absDir, "presets", path)
		overlay[overlayPath] = load.FromString(content)

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk preset files: %w", err)
	}

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

	// Detect package name from the first CUE file
	firstFileData, err := os.ReadFile(filepath.Join(dir, cueFiles[0]))
	if err != nil {
		return nil, fmt.Errorf("failed to read first CUE file: %w", err)
	}
	pkgName := detectPackageName(string(firstFileData))

	// Convert dir to absolute path for overlay (CUE requires absolute paths)
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Build overlay with _env.cue, _schema.cue, and virtual module
	overlay, err := l.buildOverlay(absDir, pkgName)
	if err != nil {
		return nil, err
	}

	// Add _env.cue and _schema.cue to the list of files to load
	cueFiles = append([]string{envOverlayFileName, schemaOverlayFileName}, cueFiles...)

	// Load CUE files with overlay
	instances := load.Instances(cueFiles, &load.Config{
		Dir:     absDir,
		Overlay: overlay,
	})

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

	// Files with a package declaration: use load.Instances() for import resolution
	if pkgName != "" {
		return l.loadFileWithInstances(path, pkgName)
	}

	// No package declaration: use CompileString() (imports not possible)
	schemaInject := schemaCUEAfterPackage()
	dataWithEnv := l.envCUE() + "\n" + schemaInject + "\n" + source

	value := l.ctx.CompileString(dataWithEnv, cue.Filename(path))
	if value.Err() != nil {
		return nil, fmt.Errorf("failed to compile CUE: %w", value.Err())
	}

	return l.parseResources(value)
}

// loadFileWithInstances loads a single CUE file using load.Instances() to support imports.
func (l *Loader) loadFileWithInstances(path, pkgName string) ([]resource.Resource, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}
	absDir := filepath.Dir(absPath)
	fileName := filepath.Base(absPath)

	overlay, err := l.buildOverlay(absDir, pkgName)
	if err != nil {
		return nil, err
	}

	files := []string{envOverlayFileName, schemaOverlayFileName, fileName}

	instances := load.Instances(files, &load.Config{
		Dir:     absDir,
		Overlay: overlay,
	})

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

// validateResource validates a resource value against the #Resource schema definition.
// If the schema is not available (e.g., for backward compatibility), it returns the original value.
func (l *Loader) validateResource(root cue.Value, field cue.Value, name string) (cue.Value, error) {
	resourceDef := root.LookupPath(cue.ParsePath("#Resource"))
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
			validated, err := l.validateResource(value, iter.Value(), "")
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
		validated, err := l.validateResource(value, value, "")
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
		validated, err := l.validateResource(value, fieldValue, iter.Selector().String())
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
