package command

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"text/template"
)

// Vars holds variables for command template substitution.
type Vars struct {
	Package string // Package path (e.g., golang.org/x/tools/gopls)
	Version string // Version string (e.g., v0.16.0)
	Name    string // Tool name (e.g., gopls)
	BinPath string // Binary path (e.g., ~/go/bin/gopls)
}

// Executor executes shell commands with variable substitution.
type Executor struct {
	workDir string
}

// NewExecutor creates a new Executor.
func NewExecutor(workDir string) *Executor {
	return &Executor{
		workDir: workDir,
	}
}

// Execute runs a command string with variable substitution.
// Variables: {{.Package}}, {{.Version}}, {{.Name}}, {{.BinPath}}
func (e *Executor) Execute(ctx context.Context, cmdStr string, vars Vars) error {
	return e.ExecuteWithEnv(ctx, cmdStr, vars, nil)
}

// ExecuteWithEnv runs a command with additional environment variables.
func (e *Executor) ExecuteWithEnv(ctx context.Context, cmdStr string, vars Vars, env map[string]string) error {
	expanded, err := e.expand(cmdStr, vars)
	if err != nil {
		return err
	}

	slog.Debug("executing command", "command", expanded)

	cmd := exec.CommandContext(ctx, "sh", "-c", expanded)

	if e.workDir != "" {
		cmd.Dir = e.workDir
	}

	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("command failed", "command", expanded, "error", err, "output", string(output))
		return fmt.Errorf("command failed: %s: %w", expanded, err)
	}

	slog.Debug("command succeeded", "command", expanded, "output", string(output))
	return nil
}

// Check runs a check command and returns true if it succeeds (exit code 0).
func (e *Executor) Check(ctx context.Context, cmdStr string, vars Vars, env map[string]string) bool {
	expanded, err := e.expand(cmdStr, vars)
	if err != nil {
		slog.Error("failed to expand command template", "error", err)
		return false
	}

	slog.Debug("checking command", "command", expanded)

	cmd := exec.CommandContext(ctx, "sh", "-c", expanded)

	if e.workDir != "" {
		cmd.Dir = e.workDir
	}

	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	return cmd.Run() == nil
}

// expand substitutes variables in the command string using text/template.
func (e *Executor) expand(cmdStr string, vars Vars) (string, error) {
	tmpl, err := template.New("cmd").Parse(cmdStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse command template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("failed to execute command template: %w", err)
	}

	return buf.String(), nil
}
