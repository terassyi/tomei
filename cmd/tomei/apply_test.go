package main

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/terassyi/tomei/internal/resource"
)

// TestFilterPrivilegedWithLog covers the --system gate that filters privileged
// resources from `tomei apply` input. Post-SUB4 the predicate auto-extends to
// both Commands and download/registry privileged tools; this test pins the
// per-resource slog `reason` attribute and the summary text.
//
// Cannot t.Parallel(): slog.SetDefault mutates global state.
// Subtests below run sequentially for the same reason — DO NOT add
// t.Parallel() inside the subtests; capture-then-cleanup breaks because
// parallel siblings would race on the same process-global default logger.
func TestFilterPrivilegedWithLog(t *testing.T) {
	capture := func(t *testing.T) *bytes.Buffer {
		t.Helper()
		// Pattern mirrors cmd/tomei/system_engine_test.go:68-77; LevelInfo
		// here because apply.go's per-resource skip log is slog.Info.
		var buf bytes.Buffer
		handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
		oldLogger := slog.Default()
		slog.SetDefault(slog.New(handler))
		t.Cleanup(func() { slog.SetDefault(oldLogger) })
		return &buf
	}

	commandsTool := func(name string, priv bool) *resource.Tool {
		return &resource.Tool{
			BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: name}},
			ToolSpec: &resource.ToolSpec{
				Commands:   &resource.ToolCommandSet{CommandSet: resource.CommandSet{Install: []string{"echo " + name}}},
				Privileged: priv,
			},
		}
	}
	downloadTool := func(name string, priv bool) *resource.Tool {
		return &resource.Tool{
			BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: name}},
			ToolSpec: &resource.ToolSpec{
				InstallerRef: "aqua",
				Version:      "1.0.0",
				Source:       &resource.DownloadSource{URL: "https://example.com/" + name + ".tar.gz"},
				Privileged:   priv,
			},
		}
	}
	registryTool := func(name string, priv bool) *resource.Tool {
		return &resource.Tool{
			BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: name}},
			ToolSpec: &resource.ToolSpec{
				InstallerRef: "aqua",
				Package:      &resource.Package{Owner: "owner", Repo: name},
				Privileged:   priv,
			},
		}
	}

	const cmdsReason = "requires sudo cache for shell commands"
	const placeReason = "places a symlink in the system bin directory requiring sudo"
	const summaryFragment = "privileged tools require sudo for cached shell commands or for placing binaries in the system bin directory"

	t.Run("privileged commands tool is filtered with commands reason", func(t *testing.T) {
		buf := capture(t)
		var w bytes.Buffer
		got := filterPrivilegedWithLog(&w, []resource.Resource{commandsTool("priv-cmds", true)})
		assert.Empty(t, got, "privileged tool must be filtered out")
		assert.Contains(t, buf.String(), "reason="+`"`+cmdsReason+`"`, "slog must carry the commands reason")
		assert.Contains(t, buf.String(), "name=priv-cmds")
		assert.Contains(t, w.String(), "1 privileged resource(s) skipped")
		assert.Contains(t, w.String(), summaryFragment)
		assert.Contains(t, w.String(), "tomei apply --system")
	})

	t.Run("privileged download tool is filtered with placement reason", func(t *testing.T) {
		buf := capture(t)
		var w bytes.Buffer
		got := filterPrivilegedWithLog(&w, []resource.Resource{downloadTool("priv-dl", true)})
		assert.Empty(t, got)
		assert.Contains(t, buf.String(), "reason="+`"`+placeReason+`"`)
		assert.Contains(t, buf.String(), "name=priv-dl")
		assert.Contains(t, w.String(), "1 privileged resource(s) skipped")
		assert.Contains(t, w.String(), summaryFragment)
	})

	t.Run("privileged registry tool uses the same placement reason as download", func(t *testing.T) {
		buf := capture(t)
		var w bytes.Buffer
		got := filterPrivilegedWithLog(&w, []resource.Resource{registryTool("priv-reg", true)})
		assert.Empty(t, got)
		assert.Contains(t, buf.String(), "reason="+`"`+placeReason+`"`)
		assert.Contains(t, buf.String(), "name=priv-reg")
	})

	t.Run("mixed privileged and non-privileged: non-privileged passes through", func(t *testing.T) {
		buf := capture(t)
		var w bytes.Buffer
		input := []resource.Resource{
			commandsTool("priv-cmds", true),
			commandsTool("plain-cmds", false),
			downloadTool("priv-dl", true),
		}
		got := filterPrivilegedWithLog(&w, input)
		assert.Len(t, got, 1)
		assert.Equal(t, "plain-cmds", got[0].Name())
		assert.Contains(t, buf.String(), "name=priv-cmds")
		assert.Contains(t, buf.String(), "name=priv-dl")
		assert.NotContains(t, buf.String(), "name=plain-cmds")
		assert.Contains(t, w.String(), "2 privileged resource(s) skipped")
	})

	t.Run("empty input emits no log and no summary", func(t *testing.T) {
		buf := capture(t)
		var w bytes.Buffer
		got := filterPrivilegedWithLog(&w, nil)
		assert.Empty(t, got)
		assert.Empty(t, buf.String())
		assert.Empty(t, w.String())
	})

	t.Run("no privileged resources emits no log and no summary", func(t *testing.T) {
		buf := capture(t)
		var w bytes.Buffer
		input := []resource.Resource{commandsTool("plain-a", false), commandsTool("plain-b", false)}
		got := filterPrivilegedWithLog(&w, input)
		assert.Len(t, got, 2)
		assert.Empty(t, buf.String(), "no slog emission expected when nothing is filtered")
		assert.Empty(t, w.String())
	})
}
