package log

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/resource"
)

func TestListSessions(t *testing.T) {
	t.Run("returns sessions sorted newest first", func(t *testing.T) {
		tmpDir := t.TempDir()

		dirs := []string{"20260201T100000", "20260203T100000", "20260202T100000"}
		for _, d := range dirs {
			require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, d), 0755))
		}

		sessions, err := ListSessions(tmpDir)
		require.NoError(t, err)
		require.Len(t, sessions, 3)

		assert.Equal(t, "20260203T100000", sessions[0].ID)
		assert.Equal(t, "20260202T100000", sessions[1].ID)
		assert.Equal(t, "20260201T100000", sessions[2].ID)

		assert.Equal(t, filepath.Join(tmpDir, "20260203T100000"), sessions[0].Dir)
	})

	t.Run("skips non-session directories", func(t *testing.T) {
		tmpDir := t.TempDir()

		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "20260201T100000"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "not-a-session"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "somefile.txt"), []byte("hi"), 0644))

		sessions, err := ListSessions(tmpDir)
		require.NoError(t, err)
		require.Len(t, sessions, 1)
		assert.Equal(t, "20260201T100000", sessions[0].ID)
	})

	t.Run("returns nil for nonexistent directory", func(t *testing.T) {
		sessions, err := ListSessions("/nonexistent/path")
		require.NoError(t, err)
		assert.Nil(t, sessions)
	})

	t.Run("returns nil for empty directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		sessions, err := ListSessions(tmpDir)
		require.NoError(t, err)
		assert.Nil(t, sessions)
	})
}

func TestReadSessionLogs(t *testing.T) {
	t.Run("reads log files", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Filenames use resource.Kind values (capitalized: Tool, Runtime)
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "Tool_ripgrep.log"), []byte("log content 1"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "Runtime_go.log"), []byte("log content 2"), 0644))

		logs, err := ReadSessionLogs(tmpDir)
		require.NoError(t, err)
		require.Len(t, logs, 2)

		// Sorted: Runtime/go, Tool/ripgrep
		assert.Equal(t, resource.KindRuntime, logs[0].Kind)
		assert.Equal(t, "go", logs[0].Name)
		assert.Equal(t, "log content 2", logs[0].Content)

		assert.Equal(t, resource.KindTool, logs[1].Kind)
		assert.Equal(t, "ripgrep", logs[1].Name)
		assert.Equal(t, "log content 1", logs[1].Content)
	})

	t.Run("skips non-log files and directories", func(t *testing.T) {
		tmpDir := t.TempDir()

		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "Tool_foo.log"), []byte("ok"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "readme.txt"), []byte("skip"), 0644))
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755))

		logs, err := ReadSessionLogs(tmpDir)
		require.NoError(t, err)
		require.Len(t, logs, 1)
		assert.Equal(t, "foo", logs[0].Name)
	})

	t.Run("skips invalid filenames", func(t *testing.T) {
		tmpDir := t.TempDir()

		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "nounderscore.log"), []byte("skip"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "Tool_valid.log"), []byte("ok"), 0644))

		logs, err := ReadSessionLogs(tmpDir)
		require.NoError(t, err)
		require.Len(t, logs, 1)
		assert.Equal(t, "valid", logs[0].Name)
	})
}

func TestReadResourceLog(t *testing.T) {
	t.Run("reads specific resource log", func(t *testing.T) {
		tmpDir := t.TempDir()

		content := "# tomei installation log\nsome output\n"
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "Tool_gopls.log"), []byte(content), 0644))

		got, err := ReadResourceLog(tmpDir, resource.KindTool, "gopls")
		require.NoError(t, err)
		assert.Equal(t, content, got)
	})

	t.Run("returns error for missing log", func(t *testing.T) {
		tmpDir := t.TempDir()

		_, err := ReadResourceLog(tmpDir, resource.KindTool, "nonexistent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no log found for Tool/nonexistent")
	})
}

func TestParseLogFilename(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		wantKind string
		wantName string
		wantOK   bool
	}{
		{
			name:     "valid tool",
			filename: "Tool_ripgrep.log",
			wantKind: "Tool",
			wantName: "ripgrep",
			wantOK:   true,
		},
		{
			name:     "valid runtime",
			filename: "Runtime_go.log",
			wantKind: "Runtime",
			wantName: "go",
			wantOK:   true,
		},
		{
			name:     "name with underscore",
			filename: "Tool_my_tool.log",
			wantKind: "Tool",
			wantName: "my_tool",
			wantOK:   true,
		},
		{
			name:     "no underscore",
			filename: "invalid.log",
			wantKind: "",
			wantName: "",
			wantOK:   false,
		},
		{
			name:     "empty kind",
			filename: "_name.log",
			wantKind: "",
			wantName: "",
			wantOK:   false,
		},
		{
			name:     "empty name",
			filename: "tool_.log",
			wantKind: "",
			wantName: "",
			wantOK:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, name, ok := parseLogFilename(tt.filename)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantKind, kind)
			assert.Equal(t, tt.wantName, name)
		})
	}
}
