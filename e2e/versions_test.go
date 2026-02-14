//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/terassyi/tomei/internal/config"
	"github.com/terassyi/tomei/internal/resource"
)

// e2eVersions holds version strings extracted from CUE manifests.
// This is the single source of truth for version assertions in E2E tests.
type e2eVersions struct {
	// Basic manifests (e2e/config/manifests/)
	GoVersion        string // runtime.cue → Runtime/go
	GoVersionUpgrade string // runtime.cue.upgrade → Runtime/go
	GhVersion        string // tools.cue → Tool/gh
	GoplsVersion     string // delegation.cue → Tool/gopls
	RustVersion      string // rust-runtime.cue → Runtime/rust
	SdVersion        string // rust-delegation.cue → Tool/sd

	// Dependency test manifests (e2e/config/dependency-test/)
	DepGoVersion  string // runtime-chain.cue → Runtime/go (1.23.5)
	DepRgVersion  string // parallel.cue → Tool/rg
	DepFdVersion  string // parallel.cue → Tool/fd
	DepBatVersion string // parallel.cue → Tool/bat
	DepJqVersion  string // toolref.cue → Tool/jq

	// Registry manifests (e2e/config/registry/)
	RegRgVersion    string // tools.cue → Tool/rg (newer)
	RegFdVersion    string // tools.cue → Tool/fd (newer)
	RegJqVersion    string // tools.cue → Tool/jq (newer)
	RegRgVersionOld string // tools.cue.old → Tool/rg (older)
	RegFdVersionOld string // tools.cue.old → Tool/fd (older)
	RegJqVersionOld string // tools.cue.old → Tool/jq (older)

	// Three-segment package test (e2e/config/three-segment-test/)
	RegLogcliVersion string // logcli.cue → Tool/logcli
}

// versions is the global version holder, populated in BeforeSuite.
var versions *e2eVersions

// loadVersions loads version strings from CUE manifests.
func loadVersions() (*e2eVersions, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return nil, fmt.Errorf("failed to get current file path")
	}
	e2eDir := filepath.Dir(filename)
	manifestsDir := filepath.Join(e2eDir, "config", "manifests")
	delegationTestDir := filepath.Join(e2eDir, "config", "delegation-test")
	depTestDir := filepath.Join(e2eDir, "config", "dependency-test")
	registryDir := filepath.Join(e2eDir, "config", "registry")
	loader := config.NewLoader(nil)
	v := &e2eVersions{}

	// Basic manifests
	if ver, err := loadVersion(loader, filepath.Join(manifestsDir, "runtime.cue"), resource.KindRuntime, "go"); err != nil {
		return nil, fmt.Errorf("runtime.cue: %w", err)
	} else {
		v.GoVersion = ver
	}

	if ver, err := loadVersion(loader, filepath.Join(manifestsDir, "runtime.cue.upgrade"), resource.KindRuntime, "go"); err != nil {
		return nil, fmt.Errorf("runtime.cue.upgrade: %w", err)
	} else {
		v.GoVersionUpgrade = ver
	}

	if ver, err := loadVersion(loader, filepath.Join(manifestsDir, "tools.cue"), resource.KindTool, "gh"); err != nil {
		return nil, fmt.Errorf("tools.cue: %w", err)
	} else {
		v.GhVersion = ver
	}

	if ver, err := loadVersion(loader, filepath.Join(manifestsDir, "delegation.cue"), resource.KindTool, "gopls"); err != nil {
		return nil, fmt.Errorf("delegation.cue: %w", err)
	} else {
		v.GoplsVersion = ver
	}

	if ver, err := loadVersion(loader, filepath.Join(delegationTestDir, "rust-runtime.cue"), resource.KindRuntime, "rust"); err != nil {
		return nil, fmt.Errorf("rust-runtime.cue: %w", err)
	} else {
		v.RustVersion = ver
	}

	if ver, err := loadVersion(loader, filepath.Join(delegationTestDir, "rust-delegation.cue"), resource.KindTool, "sd"); err != nil {
		return nil, fmt.Errorf("rust-delegation.cue: %w", err)
	} else {
		v.SdVersion = ver
	}

	// Dependency test manifests
	if ver, err := loadVersion(loader, filepath.Join(depTestDir, "runtime-chain.cue"), resource.KindRuntime, "go"); err != nil {
		return nil, fmt.Errorf("runtime-chain.cue: %w", err)
	} else {
		v.DepGoVersion = ver
	}

	if ver, err := loadVersion(loader, filepath.Join(depTestDir, "parallel.cue"), resource.KindTool, "rg"); err != nil {
		return nil, fmt.Errorf("parallel.cue rg: %w", err)
	} else {
		v.DepRgVersion = ver
	}

	if ver, err := loadVersion(loader, filepath.Join(depTestDir, "parallel.cue"), resource.KindTool, "fd"); err != nil {
		return nil, fmt.Errorf("parallel.cue fd: %w", err)
	} else {
		v.DepFdVersion = ver
	}

	if ver, err := loadVersion(loader, filepath.Join(depTestDir, "parallel.cue"), resource.KindTool, "bat"); err != nil {
		return nil, fmt.Errorf("parallel.cue bat: %w", err)
	} else {
		v.DepBatVersion = ver
	}

	if ver, err := loadVersion(loader, filepath.Join(depTestDir, "toolref.cue"), resource.KindTool, "jq"); err != nil {
		return nil, fmt.Errorf("toolref.cue jq: %w", err)
	} else {
		v.DepJqVersion = ver
	}

	// Registry manifests
	if ver, err := loadVersion(loader, filepath.Join(registryDir, "tools.cue"), resource.KindTool, "rg"); err != nil {
		return nil, fmt.Errorf("registry tools.cue rg: %w", err)
	} else {
		v.RegRgVersion = ver
	}

	if ver, err := loadVersion(loader, filepath.Join(registryDir, "tools.cue"), resource.KindTool, "fd"); err != nil {
		return nil, fmt.Errorf("registry tools.cue fd: %w", err)
	} else {
		v.RegFdVersion = strings.TrimPrefix(ver, "v")
	}

	if ver, err := loadVersion(loader, filepath.Join(registryDir, "tools.cue"), resource.KindTool, "jq"); err != nil {
		return nil, fmt.Errorf("registry tools.cue jq: %w", err)
	} else {
		v.RegJqVersion = ver
	}

	if ver, err := loadVersion(loader, filepath.Join(registryDir, "tools.cue.old"), resource.KindTool, "rg"); err != nil {
		return nil, fmt.Errorf("registry tools.cue.old rg: %w", err)
	} else {
		v.RegRgVersionOld = ver
	}

	if ver, err := loadVersion(loader, filepath.Join(registryDir, "tools.cue.old"), resource.KindTool, "fd"); err != nil {
		return nil, fmt.Errorf("registry tools.cue.old fd: %w", err)
	} else {
		v.RegFdVersionOld = strings.TrimPrefix(ver, "v")
	}

	if ver, err := loadVersion(loader, filepath.Join(registryDir, "tools.cue.old"), resource.KindTool, "jq"); err != nil {
		return nil, fmt.Errorf("registry tools.cue.old jq: %w", err)
	} else {
		v.RegJqVersionOld = ver
	}

	// Three-segment package test
	threeSegDir := filepath.Join(e2eDir, "config", "three-segment-test")

	if ver, err := loadVersion(loader, filepath.Join(threeSegDir, "logcli.cue"), resource.KindTool, "logcli"); err != nil {
		return nil, fmt.Errorf("logcli.cue: %w", err)
	} else {
		v.RegLogcliVersion = ver
	}

	return v, nil
}

// loadVersion loads a single CUE file and extracts the version of the named resource.
// For non-.cue files (e.g. .cue.upgrade, .cue.old), the content is copied to a
// temporary .cue file because LoadFile() requires the .cue extension for CUE's
// load.Instances() based loading.
func loadVersion(loader *config.Loader, path string, kind resource.Kind, name string) (string, error) {
	loadPath := path
	if filepath.Ext(path) != ".cue" {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("failed to read %s: %w", path, err)
		}
		tmpDir, err := os.MkdirTemp("", "tomei-e2e-*")
		if err != nil {
			return "", fmt.Errorf("failed to create temp dir: %w", err)
		}
		defer os.RemoveAll(tmpDir)
		loadPath = filepath.Join(tmpDir, filepath.Base(path)+".cue")
		if err := os.WriteFile(loadPath, data, 0644); err != nil {
			return "", fmt.Errorf("failed to write temp file: %w", err)
		}
	}

	resources, err := loader.LoadFile(loadPath)
	if err != nil {
		return "", fmt.Errorf("failed to load %s: %w", path, err)
	}

	for _, res := range resources {
		if res.Kind() != kind || res.Name() != name {
			continue
		}
		switch r := res.(type) {
		case *resource.Runtime:
			return r.RuntimeSpec.Version, nil
		case *resource.Tool:
			return r.ToolSpec.Version, nil
		}
	}

	return "", fmt.Errorf("%s %q not found in %s", kind, name, filepath.Base(path))
}
