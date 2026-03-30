package cuemod

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"
)

// tomeiImportBase is the module path without the major version suffix,
// used for constructing CUE import paths (e.g. "tomei.terassyi.net/presets/go").
const tomeiImportBase = "tomei.terassyi.net"

// maxModuleZipSize is the upper bound for module zip size (50 MB).
const maxModuleZipSize = 50 << 20

// maxCUEFileSize is the upper bound for a single CUE file in the zip (1 MB).
const maxCUEFileSize = 1 << 20

// fetchPresetsTimeout is the default timeout for FetchPresets network operations.
const fetchPresetsTimeout = 30 * time.Second

// PresetInfo describes a preset package and its exported definitions.
type PresetInfo struct {
	Name        string   `json:"name"`
	ImportPath  string   `json:"importPath"`
	Definitions []string `json:"definitions"`
	Source      string   `json:"source,omitempty"`
}

// definitionRe matches top-level CUE definitions (e.g. "#GoRuntime:").
// Only #-prefixed identifiers at column 0 are matched; hidden definitions (_#Name)
// and indented definitions are intentionally excluded.
var definitionRe = regexp.MustCompile(`(?m)^(#\w+):`)

// FetchPresets fetches the module zip from the OCI registry and extracts preset info.
// If version is empty, resolves to the latest version.
// Returns (presets, resolvedVersion, error).
func FetchPresets(ctx context.Context, version string, opts ...ResolveOption) (presets []PresetInfo, resolvedVersion string, err error) {
	ctx, cancel := context.WithTimeout(ctx, fetchPresetsTimeout)
	defer cancel()

	client, err := newRegistryClient()
	if err != nil {
		return nil, "", err
	}

	if version == "" {
		resolved, err := resolveLatestVersionWithClient(ctx, client, opts...)
		if err != nil {
			return nil, "", fmt.Errorf("failed to resolve latest version: %w", err)
		}
		version = resolved
	}

	modVer, err := newModuleVersion(version)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create module version: %w", err)
	}

	mod, err := client.GetModule(ctx, modVer)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get module from registry: %w", err)
	}

	zipReader, err := mod.GetZip(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get module zip: %w", err)
	}
	defer func() {
		if cerr := zipReader.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("zip integrity check failed: %w", cerr)
		}
	}()

	zipData, err := io.ReadAll(io.LimitReader(zipReader, maxModuleZipSize))
	if err != nil {
		return nil, "", fmt.Errorf("failed to read module zip: %w", err)
	}

	presets, err = extractPresetsFromZip(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, "", err
	}

	return presets, version, nil
}

// extractPresetsFromZip reads a zip archive and extracts preset info from presets/*/*.cue files.
// CUE module zips contain bare relative paths (e.g. "presets/go/go.cue").
func extractPresetsFromZip(r io.ReaderAt, size int64) ([]PresetInfo, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("failed to open zip: %w", err)
	}

	// Map preset name to collected info.
	presetMap := make(map[string]*PresetInfo)

	for _, f := range zr.File {
		// Zip entries are bare relative paths: presets/go/go.cue
		// Find the "presets/" segment.
		parts := strings.Split(f.Name, "/")
		presetsIdx := -1
		for i, p := range parts {
			if p == "presets" {
				presetsIdx = i
				break
			}
		}
		if presetsIdx < 0 {
			continue
		}

		// Expect: presets/<pkg>/<file>.cue
		if len(parts) != presetsIdx+3 {
			continue
		}
		pkgName := parts[presetsIdx+1]
		fileName := parts[presetsIdx+2]
		if !strings.HasSuffix(fileName, ".cue") {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("failed to open %s in zip: %w", f.Name, err)
		}
		content, err := io.ReadAll(io.LimitReader(rc, maxCUEFileSize))
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read %s in zip: %w", f.Name, err)
		}

		info, ok := presetMap[pkgName]
		if !ok {
			info = &PresetInfo{
				Name:       pkgName,
				ImportPath: path.Join(tomeiImportBase, "presets", pkgName),
			}
			presetMap[pkgName] = info
		}

		src := string(content)

		// Append source content.
		if info.Source != "" {
			info.Source += "\n"
		}
		info.Source += src

		// Extract exported definition names (#-prefixed, top-level only).
		matches := definitionRe.FindAllStringSubmatch(src, -1)
		for _, m := range matches {
			info.Definitions = append(info.Definitions, m[1])
		}
	}

	// Collect, deduplicate definitions, and sort.
	presets := make([]PresetInfo, 0, len(presetMap))
	for _, info := range presetMap {
		slices.Sort(info.Definitions)
		info.Definitions = slices.Compact(info.Definitions)
		presets = append(presets, *info)
	}
	slices.SortFunc(presets, func(a, b PresetInfo) int {
		return strings.Compare(a.Name, b.Name)
	})

	return presets, nil
}
