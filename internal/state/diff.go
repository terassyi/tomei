package state

import (
	"sort"

	"github.com/terassyi/tomei/internal/resource"
)

// DiffType represents the type of change between two states.
type DiffType string

const (
	DiffAdded    DiffType = "added"
	DiffRemoved  DiffType = "removed"
	DiffModified DiffType = "modified"
)

// ResourceDiff represents a single resource change.
type ResourceDiff struct {
	Kind       string   `json:"kind"`
	Name       string   `json:"name"`
	Type       DiffType `json:"type"`
	OldVersion string   `json:"oldVersion,omitempty"`
	NewVersion string   `json:"newVersion,omitempty"`
	Details    []string `json:"details,omitempty"`
}

// Diff holds the complete diff between two states.
type Diff struct {
	Changes []ResourceDiff `json:"changes"`
}

// HasChanges returns true if there are any differences.
func (d *Diff) HasChanges() bool {
	return len(d.Changes) > 0
}

// Summary returns counts of added, modified, and removed resources.
func (d *Diff) Summary() (added, modified, removed int) {
	for _, c := range d.Changes {
		switch c.Type {
		case DiffAdded:
			added++
		case DiffModified:
			modified++
		case DiffRemoved:
			removed++
		}
	}
	return
}

// DiffUserStates compares two UserStates and returns the differences.
// old is the backup (before), current is the current state (after).
func DiffUserStates(old, current *UserState) *Diff {
	diff := &Diff{}

	diffRegistry(old, current, diff)
	diffMap(old.Runtimes, current.Runtimes, "runtime", diff, compareRuntimes)
	diffMap(old.Tools, current.Tools, "tool", diff, compareTools)
	diffMap(old.InstallerRepositories, current.InstallerRepositories, "installerRepository", diff, nil)

	return diff
}

// compareFunc returns (version, details) for a resource that exists in both old and new state.
// details is non-nil only when there are modifications.
type compareFunc[V any] func(old, cur V) (oldVersion, newVersion string, details []string)

// diffMap compares two maps of the same resource type and appends diffs.
// If cmp is nil, only add/remove is detected (no modification check).
func diffMap[V any](oldMap, curMap map[string]V, kind string, diff *Diff, cmp compareFunc[V]) {
	if oldMap == nil {
		oldMap = make(map[string]V)
	}
	if curMap == nil {
		curMap = make(map[string]V)
	}

	for _, name := range collectKeys(oldMap, curMap) {
		old, inOld := oldMap[name]
		cur, inCur := curMap[name]

		switch {
		case !inOld && inCur:
			rd := ResourceDiff{Kind: kind, Name: name, Type: DiffAdded}
			if cmp != nil {
				_, rd.NewVersion, _ = cmp(old, cur)
			}
			diff.Changes = append(diff.Changes, rd)

		case inOld && !inCur:
			rd := ResourceDiff{Kind: kind, Name: name, Type: DiffRemoved}
			if cmp != nil {
				rd.OldVersion, _, _ = cmp(old, cur)
			}
			diff.Changes = append(diff.Changes, rd)

		default: // both exist
			if cmp == nil {
				continue
			}
			oldVer, newVer, details := cmp(old, cur)
			if len(details) > 0 {
				diff.Changes = append(diff.Changes, ResourceDiff{
					Kind:       kind,
					Name:       name,
					Type:       DiffModified,
					OldVersion: oldVer,
					NewVersion: newVer,
					Details:    details,
				})
			}
		}
	}
}

func compareTools(old, cur *resource.ToolState) (string, string, []string) {
	oldVer := ""
	curVer := ""
	if old != nil {
		oldVer = old.Version
	}
	if cur != nil {
		curVer = cur.Version
	}
	if old == nil || cur == nil {
		return oldVer, curVer, nil
	}

	var details []string
	if old.Version != cur.Version {
		details = append(details, "version changed")
	}
	if old.InstallPath != cur.InstallPath {
		details = append(details, "installPath changed")
	}
	if old.Digest != cur.Digest {
		details = append(details, "digest changed")
	}
	return oldVer, curVer, details
}

func compareRuntimes(old, cur *resource.RuntimeState) (string, string, []string) {
	oldVer := ""
	curVer := ""
	if old != nil {
		oldVer = old.Version
	}
	if cur != nil {
		curVer = cur.Version
	}
	if old == nil || cur == nil {
		return oldVer, curVer, nil
	}

	var details []string
	if old.Version != cur.Version {
		details = append(details, "version changed")
	}
	if old.InstallPath != cur.InstallPath {
		details = append(details, "installPath changed")
	}
	return oldVer, curVer, details
}

func diffRegistry(old, current *UserState, diff *Diff) {
	oldRef := ""
	newRef := ""
	if old.Registry != nil && old.Registry.Aqua != nil {
		oldRef = old.Registry.Aqua.Ref
	}
	if current.Registry != nil && current.Registry.Aqua != nil {
		newRef = current.Registry.Aqua.Ref
	}

	if oldRef == newRef {
		return
	}

	switch {
	case oldRef == "":
		diff.Changes = append(diff.Changes, ResourceDiff{
			Kind: "registry", Name: "aqua", Type: DiffAdded, NewVersion: newRef,
		})
	case newRef == "":
		diff.Changes = append(diff.Changes, ResourceDiff{
			Kind: "registry", Name: "aqua", Type: DiffRemoved, OldVersion: oldRef,
		})
	default:
		diff.Changes = append(diff.Changes, ResourceDiff{
			Kind: "registry", Name: "aqua", Type: DiffModified, OldVersion: oldRef, NewVersion: newRef,
		})
	}
}

// collectKeys returns the sorted union of keys from two maps.
func collectKeys[V any](a, b map[string]V) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		seen[k] = struct{}{}
	}
	for k := range b {
		seen[k] = struct{}{}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
