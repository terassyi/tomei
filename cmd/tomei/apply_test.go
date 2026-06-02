package main

import (
	"bytes"
	"log/slog"
	"strings"
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
	const summaryFragment = "privileged tools require a sudo cache for shell commands or for placing symlinks in the system bin directory"

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

	// skipMsg is the literal log message emitted by filterPrivilegedWithLog
	// per-resource. The negative-path subtests assert it's absent from the
	// captured buffer (rather than asserting buf is fully empty), so unrelated
	// concurrent slog emissions cannot induce a false failure.
	const skipMsg = "skipping privileged resource"

	t.Run("empty input emits no log and no summary", func(t *testing.T) {
		buf := capture(t)
		var w bytes.Buffer
		got := filterPrivilegedWithLog(&w, nil)
		assert.Empty(t, got)
		assert.NotContains(t, buf.String(), skipMsg, "no privileged-skip emission expected")
		assert.Empty(t, w.String())
	})

	t.Run("no privileged resources emits no log and no summary", func(t *testing.T) {
		buf := capture(t)
		var w bytes.Buffer
		input := []resource.Resource{commandsTool("plain-a", false), commandsTool("plain-b", false)}
		got := filterPrivilegedWithLog(&w, input)
		assert.Len(t, got, 2)
		assert.NotContains(t, buf.String(), skipMsg, "no privileged-skip emission expected when nothing is filtered")
		assert.Empty(t, w.String())
	})
}

// TestFilterNonPrivilegedWithLog covers --system-only's reverse filter:
// strip non-privileged user resources, keep only privileged. Same slog
// constraints as TestFilterPrivilegedWithLog — sequential subtests.
func TestFilterNonPrivilegedWithLog(t *testing.T) {
	capture := func(t *testing.T) *bytes.Buffer {
		t.Helper()
		var buf bytes.Buffer
		handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
		oldLogger := slog.Default()
		slog.SetDefault(slog.New(handler))
		t.Cleanup(func() { slog.SetDefault(oldLogger) })
		return &buf
	}

	tool := func(name string, priv bool) *resource.Tool {
		return &resource.Tool{
			BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: name}},
			ToolSpec: &resource.ToolSpec{
				Commands:   &resource.ToolCommandSet{CommandSet: resource.CommandSet{Install: []string{"echo " + name}}},
				Privileged: priv,
			},
		}
	}

	t.Run("strips non-privileged, keeps privileged", func(t *testing.T) {
		// Use disjoint names (alpha = non-priv, omega = priv) so the
		// negative assertion below is not vulnerable to slog's
		// end-of-line newline rendering: a trailing-space match like
		// "name=priv " would miss "name=priv\n" and the priv tool could
		// be silently logged without the test catching it. Substrings
		// chosen here cannot prefix each other.
		buf := capture(t)
		var w bytes.Buffer
		got := filterNonPrivilegedWithLog(&w, []resource.Resource{
			tool("alpha", false),
			tool("omega", true),
		})
		assert.Len(t, got, 1, "only the privileged tool should remain")
		assert.Equal(t, "omega", got[0].Name())
		assert.Contains(t, buf.String(), "name=alpha")
		assert.NotContains(t, buf.String(), "name=omega", "privileged tool must not be logged as skipped")
		// Exactly one "skipping non-privileged" line should appear.
		assert.Equal(t, 1, strings.Count(buf.String(), "skipping non-privileged resource"))
		assert.Contains(t, w.String(), "1 non-privileged resource(s) skipped")
		assert.Contains(t, w.String(), "--system-only restricts apply")
	})

	t.Run("all privileged: empty summary, returns unchanged set", func(t *testing.T) {
		buf := capture(t)
		var w bytes.Buffer
		got := filterNonPrivilegedWithLog(&w, []resource.Resource{
			tool("priv-1", true),
			tool("priv-2", true),
		})
		assert.Len(t, got, 2)
		assert.NotContains(t, buf.String(), "skipping non-privileged")
		assert.Empty(t, w.String())
	})

	t.Run("all non-privileged: returns empty, logs all", func(t *testing.T) {
		buf := capture(t)
		var w bytes.Buffer
		got := filterNonPrivilegedWithLog(&w, []resource.Resource{
			tool("a", false),
			tool("b", false),
		})
		assert.Empty(t, got)
		assert.Contains(t, buf.String(), "name=a")
		assert.Contains(t, buf.String(), "name=b")
		assert.Contains(t, w.String(), "2 non-privileged resource(s) skipped")
	})
}
