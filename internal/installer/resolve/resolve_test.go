package resolve

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/tomei/internal/installer/command"
)

// mockCaptureRunner is a test double for CaptureRunner.
type mockCaptureRunner struct {
	result string
	err    error
}

func (m *mockCaptureRunner) ExecuteCapture(_ context.Context, _ []string, _ command.Vars, _ map[string]string) (string, error) {
	return m.result, m.err
}

func TestResolver_Resolve_GitHubRelease(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/repo/releases/latest" {
			fmt.Fprintln(w, `{"tag_name": "v1.2.3"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	resolver := NewResolver(&mockCaptureRunner{}, srv.Client(), WithGitHubBaseURL(srv.URL))

	version, err := resolver.Resolve(context.Background(), []string{"github-release:owner/repo:v"}, command.Vars{})
	require.NoError(t, err)
	assert.Equal(t, "1.2.3", version)
}

func TestResolver_Resolve_GitHubRelease_NoPrefix(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/repo/releases/latest" {
			fmt.Fprintln(w, `{"tag_name": "1.0.0"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	resolver := NewResolver(&mockCaptureRunner{}, srv.Client(), WithGitHubBaseURL(srv.URL))

	version, err := resolver.Resolve(context.Background(), []string{"github-release:owner/repo"}, command.Vars{})
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", version)
}

func TestResolver_Resolve_HTTPText(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "go1.25.1")
		fmt.Fprintln(w, "other line")
	}))
	defer srv.Close()

	resolver := NewResolver(&mockCaptureRunner{}, srv.Client())

	cmds := []string{fmt.Sprintf("http-text:%s:go([0-9.]+)", srv.URL)}
	version, err := resolver.Resolve(context.Background(), cmds, command.Vars{})
	require.NoError(t, err)
	assert.Equal(t, "1.25.1", version)
}

func TestResolver_Resolve_HTTPText_FullMatch(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "1.25.1")
	}))
	defer srv.Close()

	resolver := NewResolver(&mockCaptureRunner{}, srv.Client())

	cmds := []string{fmt.Sprintf("http-text:%s:[0-9.]+", srv.URL)}
	version, err := resolver.Resolve(context.Background(), cmds, command.Vars{})
	require.NoError(t, err)
	assert.Equal(t, "1.25.1", version)
}

func TestResolver_Resolve_ShellCommand(t *testing.T) {
	t.Parallel()

	runner := &mockCaptureRunner{result: "3.14.0"}
	resolver := NewResolver(runner, http.DefaultClient)

	version, err := resolver.Resolve(context.Background(), []string{"tool --version"}, command.Vars{Name: "tool"})
	require.NoError(t, err)
	assert.Equal(t, "3.14.0", version)
}

func TestResolver_Resolve_ShellCommand_Error(t *testing.T) {
	t.Parallel()

	runner := &mockCaptureRunner{err: fmt.Errorf("command failed")}
	resolver := NewResolver(runner, http.DefaultClient)

	_, err := resolver.Resolve(context.Background(), []string{"tool --version"}, command.Vars{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command failed")
}

func TestResolver_Resolve_EmptyResult(t *testing.T) {
	t.Parallel()

	runner := &mockCaptureRunner{result: ""}
	resolver := NewResolver(runner, http.DefaultClient)

	_, err := resolver.Resolve(context.Background(), []string{"tool --version"}, command.Vars{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty result")
}

func TestResolver_Resolve_EmptyCommands(t *testing.T) {
	t.Parallel()

	resolver := NewResolver(&mockCaptureRunner{}, http.DefaultClient)

	_, err := resolver.Resolve(context.Background(), nil, command.Vars{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestResolver_Resolve_InvalidGitHubReleaseFormat(t *testing.T) {
	t.Parallel()

	resolver := NewResolver(&mockCaptureRunner{}, http.DefaultClient)

	_, err := resolver.Resolve(context.Background(), []string{"github-release:invalid"}, command.Vars{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid github-release format")
}

func TestResolver_Resolve_InvalidHTTPTextFormat(t *testing.T) {
	t.Parallel()

	resolver := NewResolver(&mockCaptureRunner{}, http.DefaultClient)

	_, err := resolver.Resolve(context.Background(), []string{"http-text:noscheme"}, command.Vars{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid http-text format")
}
