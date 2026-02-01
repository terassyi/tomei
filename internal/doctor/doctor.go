package doctor

import (
	"context"
	"fmt"

	"github.com/terassyi/toto/internal/path"
	"github.com/terassyi/toto/internal/resource"
	"github.com/terassyi/toto/internal/state"
)

// Doctor checks the health of the toto-managed environment.
type Doctor struct {
	paths     *path.Paths
	state     *state.UserState
	scanPaths map[string]string // category -> path
}

// Result contains the findings from a doctor check.
type Result struct {
	// UnmanagedTools maps category (runtime name or "toto") to unmanaged tools found.
	UnmanagedTools map[string][]UnmanagedTool
	// Conflicts contains tools found in multiple locations.
	Conflicts []Conflict
	// StateIssues contains state integrity problems.
	StateIssues []StateIssue
}

// UnmanagedTool represents a tool not managed by toto.
type UnmanagedTool struct {
	Name string
	Path string
}

// Conflict represents a tool found in multiple locations.
type Conflict struct {
	Name       string
	Locations  []string // e.g., ["~/.local/bin", "~/go/bin"]
	ResolvedTo string   // PATH resolves to this location
}

// StateIssueKind represents the type of state integrity issue.
type StateIssueKind string

const (
	// StateIssueMissingBinary indicates the binary file is missing.
	StateIssueMissingBinary StateIssueKind = "missing_binary"
	// StateIssueBrokenSymlink indicates the symlink target does not exist.
	StateIssueBrokenSymlink StateIssueKind = "broken_symlink"
	// StateIssueMissingInstallDir indicates the install directory is missing.
	StateIssueMissingInstallDir StateIssueKind = "missing_install_dir"
)

// StateIssue represents a state integrity problem.
type StateIssue struct {
	Kind   StateIssueKind
	Name   string // tool or runtime name
	Path   string // the path that has the issue
	Target string // symlink target (for broken_symlink)
}

// Message returns a human-readable description of the issue.
func (i StateIssue) Message() string {
	switch i.Kind {
	case StateIssueMissingBinary:
		return fmt.Sprintf("binary not found at %s", i.Path)
	case StateIssueBrokenSymlink:
		if i.Target != "" {
			return fmt.Sprintf("symlink target %s does not exist", i.Target)
		}
		return fmt.Sprintf("broken symlink at %s", i.Path)
	case StateIssueMissingInstallDir:
		return fmt.Sprintf("install directory not found at %s", i.Path)
	default:
		return fmt.Sprintf("unknown issue at %s", i.Path)
	}
}

// New creates a new Doctor.
func New(paths *path.Paths, st *state.UserState) (*Doctor, error) {
	scanPaths := make(map[string]string)

	// Add runtime-specific paths from state
	// BinDir is where runtime binaries are symlinked (e.g., ~/go/bin for go, gofmt)
	// ToolBinPath is where tools installed via the runtime go (e.g., ~/go/bin for go install tools)
	// In most cases these are the same, but we track both to handle edge cases
	if st != nil {
		for name, runtime := range st.Runtimes {
			// Use BinDir if set, otherwise fall back to ToolBinPath
			binPath := runtime.BinDir
			if binPath == "" {
				binPath = runtime.ToolBinPath
			}
			if binPath != "" {
				expanded, err := path.Expand(binPath)
				if err != nil {
					return nil, fmt.Errorf("failed to expand path for runtime %s: %w", name, err)
				}
				scanPaths[name] = expanded
			}
		}
	}

	// Always scan the toto bin directory
	scanPaths[resource.ProjectName] = paths.UserBinDir()

	return &Doctor{
		paths:     paths,
		state:     st,
		scanPaths: scanPaths,
	}, nil
}

// Check performs all health checks and returns the results.
func (d *Doctor) Check(ctx context.Context) (*Result, error) {
	result := &Result{
		UnmanagedTools: make(map[string][]UnmanagedTool),
	}

	// 1. Scan for unmanaged tools
	unmanaged, err := d.scanForUnmanaged()
	if err != nil {
		return nil, err
	}
	result.UnmanagedTools = unmanaged

	// 2. Detect conflicts
	conflicts, err := d.detectConflicts()
	if err != nil {
		return nil, err
	}
	result.Conflicts = conflicts

	// 3. Check state integrity
	issues, err := d.checkStateIntegrity()
	if err != nil {
		return nil, err
	}
	result.StateIssues = issues

	return result, nil
}

// HasIssues returns true if there are any issues found.
func (r *Result) HasIssues() bool {
	for _, tools := range r.UnmanagedTools {
		if len(tools) > 0 {
			return true
		}
	}
	return len(r.Conflicts) > 0 || len(r.StateIssues) > 0
}

// UnmanagedToolNames returns all unmanaged tool names for suggestions.
func (r *Result) UnmanagedToolNames() []string {
	var names []string
	seen := make(map[string]bool)
	for _, tools := range r.UnmanagedTools {
		for _, t := range tools {
			if !seen[t.Name] {
				names = append(names, t.Name)
				seen[t.Name] = true
			}
		}
	}
	return names
}
