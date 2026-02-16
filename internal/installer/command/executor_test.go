package command

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewExecutor(t *testing.T) {
	t.Parallel()
	e := NewExecutor("/tmp")
	assert.NotNil(t, e)
	assert.Equal(t, "/tmp", e.workDir)
}

func TestExecutor_expand(t *testing.T) {
	t.Parallel()
	e := NewExecutor("")

	tests := []struct {
		name     string
		cmdStr   string
		vars     Vars
		expected string
	}{
		{
			name:   "expand all variables",
			cmdStr: "go install {{.Package}}@{{.Version}}",
			vars: Vars{
				Package: "golang.org/x/tools/gopls",
				Version: "v0.16.0",
			},
			expected: "go install golang.org/x/tools/gopls@v0.16.0",
		},
		{
			name:   "expand name and binpath",
			cmdStr: "rm -f {{.BinPath}}/{{.Name}}",
			vars: Vars{
				Name:    "gopls",
				BinPath: "/home/user/go/bin",
			},
			expected: "rm -f /home/user/go/bin/gopls",
		},
		{
			name:     "no variables",
			cmdStr:   "echo hello",
			vars:     Vars{},
			expected: "echo hello",
		},
		{
			name:   "expand args",
			cmdStr: "uv tool install {{.Package}}=={{.Version}} {{.Args}}",
			vars: Vars{
				Package: "ansible",
				Version: "13.3.0",
				Args:    "--with-executables-from ansible-core",
			},
			expected: "uv tool install ansible==13.3.0 --with-executables-from ansible-core",
		},
		{
			name:   "args with conditional template",
			cmdStr: "go install {{.Package}}@{{.Version}}{{if .Args}} {{.Args}}{{end}}",
			vars: Vars{
				Package: "golang.org/x/tools/gopls",
				Version: "v0.16.0",
				Args:    "",
			},
			expected: "go install golang.org/x/tools/gopls@v0.16.0",
		},
		{
			name:   "empty variable values",
			cmdStr: "cmd {{.Package}} {{.Version}}",
			vars: Vars{
				Package: "",
				Version: "",
			},
			expected: "cmd  ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := e.expand(tt.cmdStr, tt.vars)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExecutor_Execute(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("successful command", func(t *testing.T) {
		t.Parallel()
		e := NewExecutor("")
		err := e.Execute(ctx, "echo hello", Vars{})
		require.NoError(t, err)
	})

	t.Run("command with variables", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "test.txt")

		e := NewExecutor("")
		err := e.Execute(ctx, "echo {{.Name}} > "+testFile, Vars{Name: "gopls"})
		require.NoError(t, err)

		content, err := os.ReadFile(testFile)
		require.NoError(t, err)
		assert.Contains(t, string(content), "gopls")
	})

	t.Run("failing command", func(t *testing.T) {
		t.Parallel()
		e := NewExecutor("")
		err := e.Execute(ctx, "exit 1", Vars{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "command failed")
	})

	t.Run("with working directory", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		e := NewExecutor(tmpDir)
		err := e.Execute(ctx, "pwd > output.txt", Vars{})
		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(tmpDir, "output.txt"))
		require.NoError(t, err)
		assert.Contains(t, string(content), tmpDir)
	})
}

func TestExecutor_ExecuteWithEnv(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tmpDir := t.TempDir()

	t.Run("with environment variables", func(t *testing.T) {
		t.Parallel()
		e := NewExecutor("")
		testFile := filepath.Join(tmpDir, "env_test.txt")

		env := map[string]string{
			"MY_VAR": "test_value",
		}

		err := e.ExecuteWithEnv(ctx, "echo $MY_VAR > "+testFile, Vars{}, env)
		require.NoError(t, err)

		content, err := os.ReadFile(testFile)
		require.NoError(t, err)
		assert.Contains(t, string(content), "test_value")
	})
}

func TestExecutor_Check(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := NewExecutor("")

	t.Run("successful check", func(t *testing.T) {
		t.Parallel()
		result := e.Check(ctx, "true", Vars{}, nil)
		assert.True(t, result)
	})

	t.Run("failing check", func(t *testing.T) {
		t.Parallel()
		result := e.Check(ctx, "false", Vars{}, nil)
		assert.False(t, result)
	})

	t.Run("check with command -v", func(t *testing.T) {
		t.Parallel()
		// sh should exist on all systems
		result := e.Check(ctx, "command -v sh", Vars{}, nil)
		assert.True(t, result)

		// nonexistent command
		result = e.Check(ctx, "command -v nonexistent_command_12345", Vars{}, nil)
		assert.False(t, result)
	})

	t.Run("check with variables", func(t *testing.T) {
		t.Parallel()
		result := e.Check(ctx, "test {{.Name}} = gopls", Vars{Name: "gopls"}, nil)
		assert.True(t, result)

		result = e.Check(ctx, "test {{.Name}} = other", Vars{Name: "gopls"}, nil)
		assert.False(t, result)
	})
}

func TestExecutor_ContextCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	e := NewExecutor("")
	err := e.Execute(ctx, "sleep 10", Vars{})
	require.Error(t, err)
}

func TestExecutor_ExecuteWithOutput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := NewExecutor("")

	t.Run("streams output lines", func(t *testing.T) {
		t.Parallel()
		var lines []string
		callback := func(line string) {
			lines = append(lines, line)
		}

		err := e.ExecuteWithOutput(ctx, "echo line1; echo line2; echo line3", Vars{}, nil, callback)
		require.NoError(t, err)
		assert.Equal(t, []string{"line1", "line2", "line3"}, lines)
	})

	t.Run("captures stderr", func(t *testing.T) {
		t.Parallel()
		var mu sync.Mutex
		var lines []string
		callback := func(line string) {
			mu.Lock()
			lines = append(lines, line)
			mu.Unlock()
		}

		err := e.ExecuteWithOutput(ctx, "echo stdout; echo stderr >&2", Vars{}, nil, callback)
		require.NoError(t, err)
		assert.Contains(t, lines, "stdout")
		assert.Contains(t, lines, "stderr")
	})

	t.Run("with variables", func(t *testing.T) {
		t.Parallel()
		var lines []string
		callback := func(line string) {
			lines = append(lines, line)
		}

		err := e.ExecuteWithOutput(ctx, "echo {{.Name}} {{.Version}}", Vars{Name: "gopls", Version: "v0.16.0"}, nil, callback)
		require.NoError(t, err)
		assert.Equal(t, []string{"gopls v0.16.0"}, lines)
	})

	t.Run("with environment variables", func(t *testing.T) {
		t.Parallel()
		var lines []string
		callback := func(line string) {
			lines = append(lines, line)
		}

		env := map[string]string{"MY_VAR": "test_value"}
		err := e.ExecuteWithOutput(ctx, "echo $MY_VAR", Vars{}, env, callback)
		require.NoError(t, err)
		assert.Equal(t, []string{"test_value"}, lines)
	})

	t.Run("nil callback drains output", func(t *testing.T) {
		t.Parallel()
		err := e.ExecuteWithOutput(ctx, "echo hello", Vars{}, nil, nil)
		require.NoError(t, err)
	})

	t.Run("failing command", func(t *testing.T) {
		t.Parallel()
		var mu sync.Mutex
		var lines []string
		callback := func(line string) {
			mu.Lock()
			lines = append(lines, line)
			mu.Unlock()
		}

		err := e.ExecuteWithOutput(ctx, "echo before_fail; exit 1", Vars{}, nil, callback)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "command failed")
		assert.Contains(t, lines, "before_fail")
	})
}

func TestExecutor_ExecuteCapture(t *testing.T) {
	t.Parallel()
	e := NewExecutor("")
	ctx := context.Background()

	t.Run("captures stdout", func(t *testing.T) {
		t.Parallel()
		result, err := e.ExecuteCapture(ctx, "echo hello", Vars{}, nil)
		require.NoError(t, err)
		assert.Equal(t, "hello", result)
	})

	t.Run("trims whitespace", func(t *testing.T) {
		t.Parallel()
		result, err := e.ExecuteCapture(ctx, "echo '  1.83.0  '", Vars{}, nil)
		require.NoError(t, err)
		assert.Equal(t, "1.83.0", result)
	})

	t.Run("with variables", func(t *testing.T) {
		t.Parallel()
		result, err := e.ExecuteCapture(ctx, "echo {{.Version}}", Vars{Version: "stable"}, nil)
		require.NoError(t, err)
		assert.Equal(t, "stable", result)
	})

	t.Run("with environment", func(t *testing.T) {
		t.Parallel()
		env := map[string]string{"MY_VER": "2.0.0"}
		result, err := e.ExecuteCapture(ctx, "echo $MY_VER", Vars{}, env)
		require.NoError(t, err)
		assert.Equal(t, "2.0.0", result)
	})

	t.Run("failing command", func(t *testing.T) {
		t.Parallel()
		_, err := e.ExecuteCapture(ctx, "exit 1", Vars{}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "command failed")
	})

	t.Run("stderr not captured in result", func(t *testing.T) {
		t.Parallel()
		result, err := e.ExecuteCapture(ctx, "echo stdout; echo stderr >&2", Vars{}, nil)
		require.NoError(t, err)
		assert.Equal(t, "stdout", result)
	})
}
