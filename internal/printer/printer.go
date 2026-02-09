package printer

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/state"
)

// rowFormatter converts a named resource state entry into table columns.
type rowFormatter[T resource.StateType] interface {
	// Headers returns the column header names.
	Headers(wide bool) []string
	// FormatRow converts a single resource into column values.
	FormatRow(name string, item *T, wide bool) []string
}

// printTable is the generic table-printing pipeline:
// filter → sort → header → rows → flush.
func printTable[T resource.StateType](w io.Writer, m map[string]*T, name string, wide bool, f rowFormatter[T]) {
	filtered := filterMap(m, name)
	if len(filtered) == 0 {
		fmt.Fprintln(w, "No resources found.")
		return
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(f.Headers(wide), "\t"))

	for _, n := range sortedKeys(filtered) {
		cols := f.FormatRow(n, filtered[n], wide)
		fmt.Fprintln(tw, strings.Join(cols, "\t"))
	}

	tw.Flush()
}

// printResources handles the json/table dispatch for any resource type.
func printResources[T resource.StateType](w io.Writer, m map[string]*T, name string, wide, jsonOut bool, f rowFormatter[T]) error {
	if jsonOut {
		return printJSON(w, filterMap(m, name))
	}
	printTable(w, m, name, wide, f)
	return nil
}

// Run executes the get command logic for a given resource type and UserState.
func Run(w io.Writer, userState *state.UserState, resType, name string, wide, jsonOut bool) error {
	switch resType {
	case "tools":
		return printResources(w, userState.Tools, name, wide, jsonOut, toolFormatter{})
	case "runtimes":
		return printResources(w, userState.Runtimes, name, wide, jsonOut, runtimeFormatter{})
	case "installers":
		return printResources(w, userState.Installers, name, wide, jsonOut, installerFormatter{})
	case "installerrepositories":
		return printResources(w, userState.InstallerRepositories, name, wide, jsonOut, installerRepoFormatter{})
	default:
		return fmt.Errorf("unknown resource type %q", resType)
	}
}

// ResolveResourceType resolves aliases to canonical resource type names.
func ResolveResourceType(s string) (string, error) {
	aliases := map[string]string{
		"tools":                 "tools",
		"tool":                  "tools",
		"runtimes":              "runtimes",
		"runtime":               "runtimes",
		"rt":                    "runtimes",
		"installers":            "installers",
		"installer":             "installers",
		"inst":                  "installers",
		"installerrepositories": "installerrepositories",
		"installerrepository":   "installerrepositories",
		"instrepo":              "installerrepositories",
	}

	resolved, ok := aliases[strings.ToLower(s)]
	if !ok {
		return "", fmt.Errorf("unknown resource type %q, valid types: tools, runtimes, installers, installerrepositories", s)
	}
	return resolved, nil
}

// Common column header constants.
const (
	colName        = "NAME"
	colVersion     = "VERSION"
	colVersionKind = "VERSION_KIND"
	colStatus      = "STATUS"
)

// Status display strings.
const (
	statusInstalled = "Installed"
	statusTainted   = "Tainted"
)

// --- Tool ---

// toolFormatter formats ToolState entries for table output.
type toolFormatter struct{}

func (toolFormatter) Headers(wide bool) []string {
	h := []string{colName, colVersion, colVersionKind, "INSTALLER/RUNTIME", colStatus}
	if wide {
		h = append(h, "PACKAGE", "BIN_PATH")
	}
	return h
}

func (toolFormatter) FormatRow(name string, t *resource.ToolState, wide bool) []string {
	ref := t.InstallerRef
	if t.RuntimeRef != "" {
		ref = t.RuntimeRef
	}
	row := []string{name, t.Version, formatVersionKind(t.VersionKind, t.SpecVersion), ref, toolStatus(t)}
	if wide {
		pkg := ""
		if t.Package != nil {
			pkg = t.Package.String()
		}
		row = append(row, pkg, t.BinPath)
	}
	return row
}

// --- Runtime ---

// runtimeFormatter formats RuntimeState entries for table output.
type runtimeFormatter struct{}

func (runtimeFormatter) Headers(wide bool) []string {
	h := []string{colName, colVersion, colVersionKind, "TYPE", colStatus}
	if wide {
		h = append(h, "INSTALL_PATH", "BINARIES")
	}
	return h
}

func (runtimeFormatter) FormatRow(name string, r *resource.RuntimeState, wide bool) []string {
	row := []string{name, r.Version, formatVersionKind(r.VersionKind, r.SpecVersion), string(r.Type), statusInstalled}
	if wide {
		row = append(row, r.InstallPath, strings.Join(r.Binaries, ","))
	}
	return row
}

// --- Installer ---

// installerFormatter formats InstallerState entries for table output.
type installerFormatter struct{}

func (installerFormatter) Headers(_ bool) []string {
	return []string{colName, colVersion, "TOOL_REF"}
}

func (installerFormatter) FormatRow(name string, i *resource.InstallerState, _ bool) []string {
	return []string{name, i.Version, i.ToolRef}
}

// --- InstallerRepository ---

// installerRepoFormatter formats InstallerRepositoryState entries for table output.
type installerRepoFormatter struct{}

func (installerRepoFormatter) Headers(_ bool) []string {
	return []string{colName, "INSTALLER", "SOURCE_TYPE", "URL"}
}

func (installerRepoFormatter) FormatRow(name string, r *resource.InstallerRepositoryState, _ bool) []string {
	return []string{name, r.InstallerRef, string(r.SourceType), r.URL}
}

// --- Helpers ---

// toolStatus returns the display status for a tool.
func toolStatus(t *resource.ToolState) string {
	if t.IsTainted() {
		return statusTainted
	}
	return statusInstalled
}

// formatVersionKind returns a display string for the version kind.
func formatVersionKind(vk resource.VersionKind, specVersion string) string {
	switch vk {
	case resource.VersionAlias:
		return fmt.Sprintf("alias(%s)", specVersion)
	default:
		return string(vk)
	}
}

// filterMap filters a map by name. If name is empty, returns the original map.
func filterMap[T resource.StateType](m map[string]*T, name string) map[string]*T {
	if name == "" {
		return m
	}
	if v, ok := m[name]; ok {
		return map[string]*T{name: v}
	}
	return nil
}

// sortedKeys returns the keys of a map sorted alphabetically.
func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// printJSON outputs a map as indented JSON.
func printJSON[T any](w io.Writer, m map[string]T) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	fmt.Fprintln(w, string(data))
	return nil
}
