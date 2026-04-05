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

// maxTotalExtractedSize is the upper bound for total extracted content from the zip (100 MB).
// This limits zip bomb impact where many small compressed entries expand to large sizes.
const maxTotalExtractedSize = 100 << 20

// fetchPresetsTimeout is the default timeout for FetchPresets network operations.
const fetchPresetsTimeout = 30 * time.Second

// PresetInfo describes a preset package and its exported definitions.
type PresetInfo struct {
	Name        string   `json:"name"`
	ImportPath  string   `json:"importPath"`
	Definitions []string `json:"definitions"`
	Source      string   `json:"source,omitempty"`
}

// KnownPresetNames returns the list of well-known preset package names.
// Used for shell completion; the canonical source is the OCI registry,
// but a network fetch at completion time is too slow.
func KnownPresetNames() []string {
	return []string{"aqua", "brew", "bun", "deno", "go", "node", "python", "rust", "zig"}
}

// definitionRe matches top-level CUE definitions (e.g. "#GoRuntime:").
// Only #-prefixed identifiers at column 0 are matched; hidden definitions (_#Name)
// and indented definitions are intentionally excluded.
var definitionRe = regexp.MustCompile(`(?m)^(#\w+):`)

// ExtractDefinition extracts a single top-level definition block from CUE source.
// defName should include the "#" prefix (e.g. "#GoRuntime").
// Returns the full definition text including the name and braces.
//
// The parser is aware of line comments (//), block comments (/* */),
// and quoted strings so that braces inside comments or string literals
// do not affect depth tracking.
func ExtractDefinition(source, defName string) (string, error) {
	lines := strings.Split(source, "\n")
	prefix := defName + ":"

	startIdx := -1
	for i, line := range lines {
		if strings.HasPrefix(line, prefix) {
			startIdx = i
			break
		}
	}
	if startIdx < 0 {
		return "", fmt.Errorf("definition %s not found", defName)
	}

	// Count braces to find the end of the definition block.
	// Skip braces inside comments and string literals.
	depth := 0
	endIdx := startIdx
	inBlock := false // tracks block comment state across lines
	seenBody := false
	for i := startIdx; i < len(lines); i++ {
		line := lines[i]
		// Check if body content exists on or after the definition line.
		if i == startIdx {
			if hasBodyContent(line[len(prefix):]) {
				seenBody = true
			}
		} else if hasBodyContent(line) {
			seenBody = true
		}

		inBlock = countBraces(line, &depth, inBlock)
		endIdx = i

		// Only stop once we've actually seen definition body content.
		// This preserves single-line forms like `#Foo: 1` and `#Foo: { x: 1 }`,
		// while allowing valid multiline formatting such as:
		//   #Foo:
		//   {
		//     x: 1
		//   }
		if depth == 0 && seenBody {
			break
		}
	}

	if depth != 0 {
		return "", fmt.Errorf("unbalanced braces in definition %s (depth %d)", defName, depth)
	}

	return strings.Join(lines[startIdx:endIdx+1], "\n"), nil
}

// hasBodyContent returns true if line contains meaningful content
// (not just whitespace, line comments, or self-contained block comments).
func hasBodyContent(line string) bool {
	inBlock := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if inBlock {
			if ch == '*' && i+1 < len(line) && line[i+1] == '/' {
				inBlock = false
				i++ // skip '/'
			}
			continue
		}
		switch {
		case ch == ' ' || ch == '\t' || ch == '\r':
			continue
		case ch == '/' && i+1 < len(line) && line[i+1] == '/':
			return false // line comment — no body content
		case ch == '/' && i+1 < len(line) && line[i+1] == '*':
			inBlock = true
			i++ // skip '*'
		default:
			return true
		}
	}
	return false
}

// countBraces counts unquoted, uncommented braces in a single line,
// updating *depth in place. inBlockComment tracks block comment state
// across lines and is returned for the caller to pass to the next line.
func countBraces(line string, depth *int, inBlockComment bool) bool {
	inString := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		// Inside block comment — look for closing */
		if inBlockComment {
			if ch == '*' && i+1 < len(line) && line[i+1] == '/' {
				inBlockComment = false
				i++ // skip '/'
			}
			continue
		}
		if inString {
			switch ch {
			case '\\':
				i++ // skip escaped character
			case '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '/':
			if i+1 < len(line) {
				switch line[i+1] {
				case '/':
					return inBlockComment // rest of line is a line comment
				case '*':
					inBlockComment = true
					i++ // skip '*'
					continue
				}
			}
		case '"':
			inString = true
		case '{':
			*depth++
		case '}':
			*depth--
			if *depth < 0 {
				return inBlockComment // unmatched closing brace
			}
		}
	}
	return inBlockComment
}

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

	zipData, err := io.ReadAll(io.LimitReader(zipReader, maxModuleZipSize+1))
	if err != nil {
		return nil, "", fmt.Errorf("failed to read module zip: %w", err)
	}
	if int64(len(zipData)) > maxModuleZipSize {
		return nil, "", fmt.Errorf("module zip exceeds size limit of %d bytes", maxModuleZipSize)
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
	var totalExtracted int64

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
		content, err := io.ReadAll(io.LimitReader(rc, maxCUEFileSize+1))
		closeErr := rc.Close()
		if err != nil {
			if closeErr != nil {
				return nil, fmt.Errorf("failed to read %s in zip: %w (and failed to close: %w)", f.Name, err, closeErr)
			}
			return nil, fmt.Errorf("failed to read %s in zip: %w", f.Name, err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("failed to close %s in zip: %w", f.Name, closeErr)
		}
		if int64(len(content)) > maxCUEFileSize {
			return nil, fmt.Errorf("CUE file %s exceeds size limit of %d bytes", f.Name, maxCUEFileSize)
		}
		totalExtracted += int64(len(content))
		if totalExtracted > maxTotalExtractedSize {
			return nil, fmt.Errorf("total extracted content exceeds size limit of %d bytes", maxTotalExtractedSize)
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
