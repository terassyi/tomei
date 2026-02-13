// Command publish-module assembles and optionally publishes the tomei CUE module
// to an OCI registry. It collects presets and schema from the repository, builds
// a CUE module zip, and pushes it as an OCI artifact.
//
// Usage:
//
//	go run ./hack/publish-module --version v0.0.1 --registry ghcr.io/terassyi
//	go run ./hack/publish-module --version v0.0.1 --assemble-only --output-dir ./out
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"cuelabs.dev/go/oci/ociregistry/ociauth"
	"cuelabs.dev/go/oci/ociregistry/ociclient"

	"cuelang.org/go/mod/modregistry"
	"cuelang.org/go/mod/module"
	"cuelang.org/go/mod/modzip"
)

const (
	modulePath = "tomei.terassyi.net@v0"
)

func main() {
	var (
		version      string
		registry     string
		assembleDir  string
		assembleOnly bool
	)

	flag.StringVar(&version, "version", "", "Module version (e.g., v0.0.1) (required)")
	flag.StringVar(&registry, "registry", "ghcr.io/terassyi", "OCI registry host/path")
	flag.StringVar(&assembleDir, "output-dir", "", "Directory to write assembled module (default: temp dir)")
	flag.BoolVar(&assembleOnly, "assemble-only", false, "Only assemble the module directory, do not push")
	flag.Parse()

	if version == "" {
		log.Fatal("--version is required")
	}

	// Find the repository root
	repoRoot, err := findRepoRoot()
	if err != nil {
		log.Fatalf("Failed to find repository root: %v", err)
	}

	// Build module files
	files, err := buildModuleFiles(repoRoot)
	if err != nil {
		log.Fatalf("Failed to build module files: %v", err)
	}

	log.Printf("Assembled module %s@%s with %d files", modulePath, version, len(files))
	for p := range files {
		log.Printf("  %s", p)
	}

	// If assemble-only, write to directory and exit
	if assembleOnly {
		outDir := assembleDir
		if outDir == "" {
			outDir, err = os.MkdirTemp("", "tomei-module-*")
			if err != nil {
				log.Fatalf("Failed to create temp dir: %v", err)
			}
		}

		if err := writeModuleDir(outDir, files); err != nil {
			log.Fatalf("Failed to write module directory: %v", err)
		}
		log.Printf("Module assembled at: %s", outDir)
		return
	}

	// Create module zip and push to registry
	if err := pushModule(version, registry, files); err != nil {
		log.Fatalf("Failed to publish module: %v", err)
	}

	log.Printf("Published %s@%s to %s", modulePath, version, registry)
}

// pushModule creates a module zip from the given files and pushes it to the OCI registry.
func pushModule(version, registry string, files map[string][]byte) error {
	mv, err := module.NewVersion(modulePath, version)
	if err != nil {
		return fmt.Errorf("failed to create module version: %w", err)
	}

	var zipBuf bytes.Buffer
	memFiles := make([]memFile, 0, len(files))
	for p, data := range files {
		memFiles = append(memFiles, memFile{path: p, content: data})
	}
	if err := modzip.Create(&zipBuf, mv, memFiles, memFileIO{}); err != nil { //nolint:staticcheck // modzip.Create is the current API
		return fmt.Errorf("failed to create module zip: %w", err)
	}
	log.Printf("Created module zip: %d bytes", zipBuf.Len())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	authConfig, err := ociauth.Load(nil)
	if err != nil {
		return fmt.Errorf("failed to load OCI auth config: %w", err)
	}
	transport := ociauth.NewStdTransport(ociauth.StdTransportParams{
		Config: authConfig,
	})
	ociClient, err := ociclient.New(registry, &ociclient.Options{
		Transport: transport,
	})
	if err != nil {
		return fmt.Errorf("failed to create OCI client for %s: %w", registry, err)
	}

	client := modregistry.NewClient(ociClient)
	zipBytes := zipBuf.Bytes()
	return client.PutModule(ctx, mv, bytes.NewReader(zipBytes), int64(len(zipBytes)))
}

// findRepoRoot walks up from the current directory to find the repository root
// (identified by the go.mod file).
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find repository root (no go.mod found)")
		}
		dir = parent
	}
}

// buildModuleFiles collects all files for the CUE module from the repository.
func buildModuleFiles(repoRoot string) (map[string][]byte, error) {
	files := make(map[string][]byte)

	// Add cue.mod/module.cue
	moduleCUE := fmt.Sprintf("module: %q\nlanguage: version: \"v0.9.0\"\n", modulePath)
	files["cue.mod/module.cue"] = []byte(moduleCUE)

	// Add schema
	schemaPath := filepath.Join(repoRoot, "internal", "config", "schema", "schema.cue")
	schemaData, err := os.ReadFile(schemaPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema: %w", err)
	}
	files["schema/schema.cue"] = schemaData

	// Add presets
	presetsDir := filepath.Join(repoRoot, "presets")
	err = filepath.WalkDir(presetsDir, func(fpath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(fpath, ".cue") {
			return nil
		}

		data, err := os.ReadFile(fpath)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", fpath, err)
		}

		rel, err := filepath.Rel(repoRoot, fpath)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}

		// Use forward slashes for module paths
		files[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk presets: %w", err)
	}

	return files, nil
}

// writeModuleDir writes the module files to a directory.
func writeModuleDir(dir string, files map[string][]byte) error {
	for p, data := range files {
		fullPath := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", p, err)
		}
		if err := os.WriteFile(fullPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", p, err)
		}
	}
	return nil
}

// memFile holds an in-memory file for modzip.Create.
type memFile struct {
	path    string
	content []byte
}

// memFileIO implements modzip.FileIO[memFile].
type memFileIO struct{}

func (memFileIO) Path(f memFile) string { return f.path }

func (memFileIO) Lstat(f memFile) (os.FileInfo, error) {
	return memFileInfo{name: path.Base(f.path), size: int64(len(f.content))}, nil
}

func (memFileIO) Open(f memFile) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f.content)), nil
}

type memFileInfo struct {
	name string
	size int64
}

func (fi memFileInfo) Name() string      { return fi.name }
func (fi memFileInfo) Size() int64       { return fi.size }
func (fi memFileInfo) Mode() os.FileMode { return 0o644 }
func (fi memFileInfo) ModTime() time.Time {
	return time.Time{}
}
func (fi memFileInfo) IsDir() bool { return false }
func (fi memFileInfo) Sys() any    { return nil }
