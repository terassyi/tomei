package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"

	"github.com/terassyi/toto/internal/resource"
)

const (
	// ConfigFileName is the name of the toto config file that should be ignored when loading manifests.
	ConfigFileName = "config.cue"
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

// Load loads CUE configuration from the given directory.
// config.cue files are excluded from loading as they contain toto configuration, not manifests.
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

	// Load CUE files
	instances := load.Instances(cueFiles, &load.Config{
		Dir: dir,
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

	// Inject environment variables as a separate value
	// Note: We don't inject _env here as it causes issues with cue.Concrete validation

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
func (l *Loader) LoadFile(path string) ([]resource.Resource, error) {
	// Skip config.cue file
	if filepath.Base(path) == ConfigFileName {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	value := l.ctx.CompileBytes(data, cue.Filename(path))
	if value.Err() != nil {
		return nil, fmt.Errorf("failed to compile CUE: %w", value.Err())
	}

	return l.parseResources(value)
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
