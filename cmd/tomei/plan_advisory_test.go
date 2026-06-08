package main

import (
	"bytes"
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/terassyi/tomei/internal/age"
	"github.com/terassyi/tomei/internal/graph"
	"github.com/terassyi/tomei/internal/resource"
)

// fakeFetcher is a deterministic age.Fetcher for advisory tests (copied from the
// engine's pattern — cross-package unexported types can't be reused).
type fakeFetcher struct {
	times map[age.Key]time.Time
	errs  map[age.Key]error
	calls atomic.Int64
}

func (f *fakeFetcher) Fetch(_ context.Context, k age.Key) (time.Time, bool, error) {
	f.calls.Add(1)
	if e, ok := f.errs[k]; ok {
		return time.Time{}, false, e
	}
	if t, ok := f.times[k]; ok {
		return t, true, nil
	}
	return time.Time{}, false, nil
}

func aquaOverride() *resource.Installer {
	return &resource.Installer{
		BaseResource:  resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindInstaller, Metadata: resource.Metadata{Name: resource.InstallerNameAqua}},
		InstallerSpec: &resource.InstallerSpec{Type: resource.InstallTypeDownload, MinimumReleaseAge: "168h"},
	}
}

// aquaTool builds a registry (aqua) tool for cli/cli at the given version.
func aquaTool(name, version string) *resource.Tool {
	return &resource.Tool{
		BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindTool, Metadata: resource.Metadata{Name: name}},
		ToolSpec:     &resource.ToolSpec{InstallerRef: resource.InstallerNameAqua, Version: version, Package: &resource.Package{Owner: "cli", Repo: "cli"}},
	}
}

func infoWith(action resource.ActionType) (graph.NodeID, map[graph.NodeID]graph.ResourceInfo) {
	id := graph.NewNodeID(resource.KindTool, "gh")
	return id, map[graph.NodeID]graph.ResourceInfo{
		id: {Kind: resource.KindTool, Name: "gh", Action: action},
	}
}

func TestAnnotateAdvisory_SetsNote_WhenYounger(t *testing.T) {
	t.Parallel()
	key := age.Key{Source: age.SourceAquaGitHubReleases, Owner: "cli", Repo: "cli", Tag: "v2.0.0"}
	f := &fakeFetcher{times: map[age.Key]time.Time{key: time.Now().Add(-24 * time.Hour)}}
	id, info := infoWith(resource.ActionInstall)
	resources := []resource.Resource{aquaOverride(), aquaTool("gh", "v2.0.0")}

	annotateReleaseAgeAdvisoryWith(context.Background(), io.Discard, resources, info, f)

	assert.NotEmpty(t, info[id].Note)
	assert.Contains(t, info[id].Note, "released")
	assert.Contains(t, info[id].Note, "requires 168h0m0s")
}

func TestAnnotateAdvisory_SetsNote_DownloadLastModified(t *testing.T) {
	t.Parallel()
	url := "https://example.com/rg.tar.gz"
	key := age.Key{Source: age.SourceLastModified, URL: url}
	f := &fakeFetcher{times: map[age.Key]time.Time{key: time.Now().Add(-1 * time.Hour)}}
	id := graph.NewNodeID(resource.KindTool, "rg")
	info := map[graph.NodeID]graph.ResourceInfo{id: {Kind: resource.KindTool, Name: "rg", Action: resource.ActionInstall}}
	resources := []resource.Resource{
		&resource.Installer{BaseResource: resource.BaseResource{ResourceKind: resource.KindInstaller, Metadata: resource.Metadata{Name: resource.InstallerNameDownload}}, InstallerSpec: &resource.InstallerSpec{Type: resource.InstallTypeDownload, MinimumReleaseAge: "168h"}},
		&resource.Tool{BaseResource: resource.BaseResource{ResourceKind: resource.KindTool, Metadata: resource.Metadata{Name: "rg"}}, ToolSpec: &resource.ToolSpec{InstallerRef: resource.InstallerNameDownload, Version: "14.0.0", Source: &resource.DownloadSource{URL: url}}},
	}

	annotateReleaseAgeAdvisoryWith(context.Background(), io.Discard, resources, info, f)
	assert.NotEmpty(t, info[id].Note)
}

func TestAnnotateAdvisory_SetsNote_OnUpgrade(t *testing.T) {
	t.Parallel()
	key := age.Key{Source: age.SourceAquaGitHubReleases, Owner: "cli", Repo: "cli", Tag: "v2.0.0"}
	f := &fakeFetcher{times: map[age.Key]time.Time{key: time.Now().Add(-24 * time.Hour)}}
	id, info := infoWith(resource.ActionUpgrade)
	annotateReleaseAgeAdvisoryWith(context.Background(), io.Discard, []resource.Resource{aquaOverride(), aquaTool("gh", "v2.0.0")}, info, f)
	assert.NotEmpty(t, info[id].Note)
}

func TestAnnotateAdvisory_NoNote_WhenOlder(t *testing.T) {
	t.Parallel()
	key := age.Key{Source: age.SourceAquaGitHubReleases, Owner: "cli", Repo: "cli", Tag: "v2.0.0"}
	f := &fakeFetcher{times: map[age.Key]time.Time{key: time.Now().Add(-200 * time.Hour)}}
	id, info := infoWith(resource.ActionInstall)
	annotateReleaseAgeAdvisoryWith(context.Background(), io.Discard, []resource.Resource{aquaOverride(), aquaTool("gh", "v2.0.0")}, info, f)
	assert.Empty(t, info[id].Note)
}

func TestAnnotateAdvisory_NoFetch_WhenNoGatedTools(t *testing.T) {
	t.Parallel()
	// No Installer/aqua override → no threshold → gate disabled.
	f := &fakeFetcher{}
	id, info := infoWith(resource.ActionInstall)
	annotateReleaseAgeAdvisoryWith(context.Background(), io.Discard, []resource.Resource{aquaTool("gh", "v2.0.0")}, info, f)
	assert.Empty(t, info[id].Note)
	assert.Equal(t, int64(0), f.calls.Load())
}

func TestAnnotateAdvisory_NoFetch_WhenActionNone(t *testing.T) {
	t.Parallel()
	f := &fakeFetcher{}
	for _, action := range []resource.ActionType{resource.ActionNone, resource.ActionSkip} {
		id, info := infoWith(action)
		annotateReleaseAgeAdvisoryWith(context.Background(), io.Discard, []resource.Resource{aquaOverride(), aquaTool("gh", "v2.0.0")}, info, f)
		assert.Empty(t, info[id].Note)
	}
	assert.Equal(t, int64(0), f.calls.Load(), "non-change actions must not fetch")
}

func TestAnnotateAdvisory_SkipsUnresolvedTag(t *testing.T) {
	t.Parallel()
	f := &fakeFetcher{}
	id, info := infoWith(resource.ActionInstall)
	annotateReleaseAgeAdvisoryWith(context.Background(), io.Discard, []resource.Resource{aquaOverride(), aquaTool("gh", "latest")}, info, f)
	assert.Empty(t, info[id].Note)
	assert.Equal(t, int64(0), f.calls.Load(), "unresolved tag must not fetch")
}

// On a fetch error the install proceeds (no Note) AND the fail-open is made
// visible via the aggregate stderr note — it must not be silent.
func TestAnnotateAdvisory_FailOpen_OnError(t *testing.T) {
	t.Parallel()
	key := age.Key{Source: age.SourceAquaGitHubReleases, Owner: "cli", Repo: "cli", Tag: "v2.0.0"}
	f := &fakeFetcher{errs: map[age.Key]error{key: assert.AnError}}
	id, info := infoWith(resource.ActionInstall)
	var errW bytes.Buffer
	annotateReleaseAgeAdvisoryWith(context.Background(), &errW, []resource.Resource{aquaOverride(), aquaTool("gh", "v2.0.0")}, info, f)
	assert.Empty(t, info[id].Note)
	assert.Contains(t, errW.String(), "could not be computed for 1 tool(s)")
}

// A clean fetch that reports ok=false (source has no timestamp) is also a
// fail-open: no Note, surfaced as unverified — distinct from the error path.
func TestAnnotateAdvisory_NoNote_WhenSourceUnavailable(t *testing.T) {
	t.Parallel()
	// Key mapped into neither times nor errs → fakeFetcher returns (zero, false, nil).
	f := &fakeFetcher{}
	id, info := infoWith(resource.ActionInstall)
	var errW bytes.Buffer
	annotateReleaseAgeAdvisoryWith(context.Background(), &errW, []resource.Resource{aquaOverride(), aquaTool("gh", "v2.0.0")}, info, f)
	assert.Empty(t, info[id].Note)
	assert.Equal(t, int64(1), f.calls.Load(), "source-unavailable still fetches once")
	assert.Contains(t, errW.String(), "could not be computed")
}

func TestAnnotateAdvisory_DedupsSharedKey_AnnotatesBoth(t *testing.T) {
	t.Parallel()
	key := age.Key{Source: age.SourceAquaGitHubReleases, Owner: "cli", Repo: "cli", Tag: "v2.0.0"}
	f := &fakeFetcher{times: map[age.Key]time.Time{key: time.Now().Add(-24 * time.Hour)}}
	idA := graph.NewNodeID(resource.KindTool, "gh-a")
	idB := graph.NewNodeID(resource.KindTool, "gh-b")
	info := map[graph.NodeID]graph.ResourceInfo{
		idA: {Kind: resource.KindTool, Name: "gh-a", Action: resource.ActionInstall},
		idB: {Kind: resource.KindTool, Name: "gh-b", Action: resource.ActionInstall},
	}
	resources := []resource.Resource{aquaOverride(), aquaTool("gh-a", "v2.0.0"), aquaTool("gh-b", "v2.0.0")}

	annotateReleaseAgeAdvisoryWith(context.Background(), io.Discard, resources, info, f)

	assert.NotEmpty(t, info[idA].Note)
	assert.NotEmpty(t, info[idB].Note)
	assert.Equal(t, int64(1), f.calls.Load(), "shared key must be fetched once")
}
