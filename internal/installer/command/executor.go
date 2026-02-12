package command

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"text/template"
)

// OutputCallback is called for each line of command output.
type OutputCallback func(line string)

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

// ExecuteWithOutput runs a command and streams output line by line via callback.
// This is useful for displaying real-time command output (e.g., go install progress).
func (e *Executor) ExecuteWithOutput(ctx context.Context, cmdStr string, vars Vars, env map[string]string, callback OutputCallback) error {
	expanded, err := e.expand(cmdStr, vars)
	if err != nil {
		return err
	}

	slog.Debug("executing command with output", "command", expanded)

	cmd := exec.CommandContext(ctx, "sh", "-c", expanded)

	if e.workDir != "" {
		cmd.Dir = e.workDir
	}

	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Create pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	// Stream stdout and stderr concurrently.
	// io.MultiReader reads sequentially (stdout fully, then stderr), which blocks
	// stderr until stdout EOF. Since tools like `go install` write progress to
	// stderr while stdout is empty, we must read both pipes in parallel.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		e.streamOutput(stdout, callback)
	}()
	go func() {
		defer wg.Done()
		e.streamOutput(stderr, callback)
	}()
	wg.Wait()

	// Wait for command to finish
	if err := cmd.Wait(); err != nil {
		slog.Error("command failed", "command", expanded, "error", err)
		return fmt.Errorf("command failed: %s: %w", expanded, err)
	}

	slog.Debug("command succeeded", "command", expanded)
	return nil
}

// streamOutput reads from reader and calls callback for each line.
func (e *Executor) streamOutput(r io.Reader, callback OutputCallback) {
	if callback == nil {
		// Drain the reader if no callback
		_, _ = io.Copy(io.Discard, r)
		return
	}

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		callback(scanner.Text())
	}
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

// ExecuteCapture runs a command and returns its stdout as a trimmed string.
// Useful for commands that output a single value (e.g., version resolution).
func (e *Executor) ExecuteCapture(ctx context.Context, cmdStr string, vars Vars, env map[string]string) (string, error) {
	expanded, err := e.expand(cmdStr, vars)
	if err != nil {
		return "", err
	}

	slog.Debug("executing command (capture)", "command", expanded)

	cmd := exec.CommandContext(ctx, "sh", "-c", expanded)

	if e.workDir != "" {
		cmd.Dir = e.workDir
	}

	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		slog.Error("command failed", "command", expanded, "error", err, "stderr", stderr.String())
		return "", fmt.Errorf("command failed: %s: %w", expanded, err)
	}

	result := strings.TrimSpace(stdout.String())
	slog.Debug("command captured output", "command", expanded, "output", result)
	return result, nil
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
