// Package command provides a command executor for running user-defined
// shell commands during resource installation.
//
// Security Model:
// Commands are executed via "sh -c" with template-expanded strings.
// This design assumes that command templates originate from trusted,
// user-authored CUE manifests — the same trust boundary as a Makefile
// or shell script. It is intended for scenarios where the user who writes
// the manifest is the same user who runs tomei, and the manifests are not
// obtained from untrusted parties or unreviewed sources (e.g., arbitrary
// PRs or shared repos). Running tomei with untrusted manifests can
// introduce command injection risks and is not supported.
//
// Privileged Tools (--system mode):
// Commands always run as the invoking user via "sh -c"; tomei does not
// wrap commands in sudo. When --system is active, apply pre-acquires a
// sudo timestamp and refreshes it during the session (keepalive). "sudo
// ..." invocations inside a user-authored command can then use that
// cached ticket (including "sudo -n"), subject to the host's sudoers
// policy (tty_tickets, requiretty, timestamp_timeout, targetpw, etc.) and
// to the keepalive successfully refreshing the ticket. Tools that need a
// specific step to run as root should write that step explicitly as
// "sudo <cmd>". Installers that run as the user and escalate internally
// (e.g., Homebrew) work with this approach.
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
	// Args holds additional arguments for the installation command (space-joined).
	// Security: values are sourced from the user's own CUE manifest,
	// not from external/untrusted input.
	Args string
	// MinimumReleaseAge is the Go duration string declared on the owning
	// Installer/Runtime spec (e.g., "168h"). Empty when unset. Threaded by
	// the installer engine; currently a no-op until issue #257's apply-time
	// gate consumes it. The slot exists here so delegation commands can
	// reference {{.MinimumReleaseAge}} without further plumbing once the
	// gate lands.
	MinimumReleaseAge string
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

// expandCommands joins multiple commands with " && " and applies template variable substitution.
func (e *Executor) expandCommands(cmds []string, vars Vars) (string, error) {
	return e.expand(strings.Join(cmds, " && "), vars)
}

// buildCommand creates an exec.Cmd with the expanded command string, working directory, and environment.
// Commands always run as the invoking user via "sh -c"; see the package doc
// comment for how privileged tools interact with a cached sudo timestamp.
func (e *Executor) buildCommand(ctx context.Context, expanded string, env map[string]string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "sh", "-c", expanded)
	if e.workDir != "" {
		cmd.Dir = e.workDir
	}
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	return cmd
}

// Execute runs command(s) with variable substitution.
// Multiple commands are joined with " && ".
func (e *Executor) Execute(ctx context.Context, cmds []string, vars Vars) error {
	return e.ExecuteWithEnv(ctx, cmds, vars, nil)
}

// ExecuteWithOutput runs command(s) and streams output line by line via callback.
// This is useful for displaying real-time command output (e.g., go install progress).
func (e *Executor) ExecuteWithOutput(ctx context.Context, cmds []string, vars Vars, env map[string]string, callback OutputCallback) error {
	expanded, err := e.expandCommands(cmds, vars)
	if err != nil {
		return err
	}

	slog.Debug("executing command with output", "command", expanded)

	cmd := e.buildCommand(ctx, expanded, env)

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

// ExecuteWithEnv runs command(s) with additional environment variables.
func (e *Executor) ExecuteWithEnv(ctx context.Context, cmds []string, vars Vars, env map[string]string) error {
	expanded, err := e.expandCommands(cmds, vars)
	if err != nil {
		return err
	}

	slog.Debug("executing command", "command", expanded)

	cmd := e.buildCommand(ctx, expanded, env)

	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("command failed", "command", expanded, "error", err, "output", string(output))
		return fmt.Errorf("command failed: %s: %w", expanded, err)
	}

	slog.Debug("command succeeded", "command", expanded, "output", string(output))
	return nil
}

// Check runs a check command and returns true if it succeeds (exit code 0).
func (e *Executor) Check(ctx context.Context, cmds []string, vars Vars, env map[string]string) bool {
	expanded, err := e.expandCommands(cmds, vars)
	if err != nil {
		slog.Error("failed to expand command template", "error", err)
		return false
	}

	slog.Debug("checking command", "command", expanded)

	cmd := e.buildCommand(ctx, expanded, env)

	return cmd.Run() == nil
}

// ExecuteCapture runs command(s) and returns stdout as a trimmed string.
// Useful for commands that output a single value (e.g., version resolution).
func (e *Executor) ExecuteCapture(ctx context.Context, cmds []string, vars Vars, env map[string]string) (string, error) {
	expanded, err := e.expandCommands(cmds, vars)
	if err != nil {
		return "", err
	}

	slog.Debug("executing command (capture)", "command", expanded)

	cmd := e.buildCommand(ctx, expanded, env)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// ExecuteCapture is commonly used for check/query commands where a
		// non-zero exit is a normal "negative result" (e.g., `dpkg-query` exit
		// 1 = "package not installed yet" — apt's installer maps it to
		// `(false, nil)` and proceeds with install). Log at Debug so the
		// engine doesn't spam ERROR for routine negatives; callers that need
		// to surface a real failure already wrap and re-log the returned
		// error themselves.
		slog.Debug("capture command failed", "command", expanded, "error", err, "stderr", stderr.String())
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
