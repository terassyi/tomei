package command

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewExecutor(t *testing.T) {
	e := NewExecutor("/tmp")
	assert.NotNil(t, e)
	assert.Equal(t, "/tmp", e.workDir)
}

func TestExecutor_expand(t *testing.T) {
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
			result, err := e.expand(tt.cmdStr, tt.vars)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExecutor_Execute(t *testing.T) {
	ctx := context.Background()

	t.Run("successful command", func(t *testing.T) {
		e := NewExecutor("")
		err := e.Execute(ctx, "echo hello", Vars{})
		require.NoError(t, err)
	})

	t.Run("command with variables", func(t *testing.T) {
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
		e := NewExecutor("")
		err := e.Execute(ctx, "exit 1", Vars{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "command failed")
	})

	t.Run("with working directory", func(t *testing.T) {
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
	ctx := context.Background()
	tmpDir := t.TempDir()

	t.Run("with environment variables", func(t *testing.T) {
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
	ctx := context.Background()
	e := NewExecutor("")

	t.Run("successful check", func(t *testing.T) {
		result := e.Check(ctx, "true", Vars{}, nil)
		assert.True(t, result)
	})

	t.Run("failing check", func(t *testing.T) {
		result := e.Check(ctx, "false", Vars{}, nil)
		assert.False(t, result)
	})

	t.Run("check with command -v", func(t *testing.T) {
		// sh should exist on all systems
		result := e.Check(ctx, "command -v sh", Vars{}, nil)
		assert.True(t, result)

		// nonexistent command
		result = e.Check(ctx, "command -v nonexistent_command_12345", Vars{}, nil)
		assert.False(t, result)
	})

	t.Run("check with variables", func(t *testing.T) {
		result := e.Check(ctx, "test {{.Name}} = gopls", Vars{Name: "gopls"}, nil)
		assert.True(t, result)

		result = e.Check(ctx, "test {{.Name}} = other", Vars{Name: "gopls"}, nil)
		assert.False(t, result)
	})
}

func TestExecutor_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	e := NewExecutor("")
	err := e.Execute(ctx, "sleep 10", Vars{})
	require.Error(t, err)
}
