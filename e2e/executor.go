//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	. "github.com/onsi/ginkgo/v2"
)

// executor is the interface for executing commands in E2E tests.
// It abstracts the difference between container and native execution modes.
type executor interface {
	// Exec executes a command and returns combined stdout/stderr output.
	Exec(name string, args ...string) (string, error)
	// ExecBash executes a bash script and returns combined stdout/stderr output.
	ExecBash(script string) (string, error)
	// Setup prepares the environment (called in BeforeSuite).
	Setup() error
	// Cleanup cleans up the environment (called in AfterSuite).
	Cleanup() error
	// Setenv sets an environment variable for subsequent command executions.
	Setenv(key, value string)
	// Getenv gets an environment variable value.
	Getenv(key string) string
}

// ExecApply runs "tomei apply --yes" and dumps "tomei logs" on failure for diagnostics.
func ExecApply(e executor, args ...string) (string, error) {
	applyArgs := append([]string{"apply", "--yes"}, args...)
	output, err := e.Exec("tomei", applyArgs...)
	if err != nil {
		fmt.Fprintf(GinkgoWriter, "\n=== tomei apply failed, dumping logs ===\n")
		logsOutput, logsErr := e.Exec("tomei", "logs", "--list")
		if logsErr == nil {
			fmt.Fprintf(GinkgoWriter, "%s\n", logsOutput)
			lines := strings.Split(strings.TrimSpace(logsOutput), "\n")
			if len(lines) > 0 && lines[0] != "" {
				sessionLogs, _ := e.Exec("tomei", "logs", "--session", lines[0])
				fmt.Fprintf(GinkgoWriter, "%s\n", sessionLogs)
			}
		}
		fmt.Fprintf(GinkgoWriter, "=== end of tomei logs ===\n\n")
	}
	return output, err
}

// containerExecutor executes commands inside a Docker container.
type containerExecutor struct {
	containerName string
	envVars       map[string]string
}

func (e *containerExecutor) Exec(name string, args ...string) (string, error) {
	cmdArgs := append([]string{"exec", e.containerName, name}, args...)
	cmd := exec.Command("docker", cmdArgs...)
	output, err := cmd.CombinedOutput()
	// Only output tomei commands to GinkgoWriter
	if name == "tomei" {
		fmt.Fprintf(GinkgoWriter, "$ %s %v\n%s", name, args, output)
		if err != nil {
			fmt.Fprintf(GinkgoWriter, "Error: %v\n", err)
		}
	}
	return string(output), err
}

func (e *containerExecutor) ExecBash(script string) (string, error) {
	// Prepend environment variable exports if any
	if len(e.envVars) > 0 {
		var exports []string
		for k, v := range e.envVars {
			exports = append(exports, fmt.Sprintf("export %s=%q", k, v))
		}
		script = strings.Join(exports, " && ") + " && " + script
	}
	return e.Exec("bash", "-c", script)
}

func (e *containerExecutor) Setup() error {
	// Verify container is running
	cmd := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", e.containerName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("container %s is not running: %w", e.containerName, err)
	}
	if strings.TrimSpace(string(output)) != "true" {
		return fmt.Errorf("container %s is not running", e.containerName)
	}
	return nil
}

func (e *containerExecutor) Cleanup() error {
	// Container cleanup is handled by Makefile
	return nil
}

func (e *containerExecutor) Setenv(key, value string) {
	if e.envVars == nil {
		e.envVars = make(map[string]string)
	}
	e.envVars[key] = value
}

func (e *containerExecutor) Getenv(key string) string {
	if e.envVars == nil {
		return ""
	}
	return e.envVars[key]
}

// nativeExecutor executes commands directly on the host machine.
// It uses a temporary HOME directory to isolate test state.
type nativeExecutor struct {
	testHome    string            // Temporary HOME directory for test isolation
	tomeiBinary string            // Path to tomei binary
	envVars     map[string]string // Additional environment variables
}

func (e *nativeExecutor) Exec(name string, args ...string) (string, error) {
	var cmd *exec.Cmd
	if name == "tomei" {
		cmd = exec.Command(e.tomeiBinary, args...)
	} else {
		cmd = exec.Command(name, args...)
	}
	cmd.Env = e.buildEnv()
	output, err := cmd.CombinedOutput()
	// Only output tomei commands to GinkgoWriter
	if name == "tomei" {
		fmt.Fprintf(GinkgoWriter, "$ %s %v\n%s", name, args, output)
		if err != nil {
			fmt.Fprintf(GinkgoWriter, "Error: %v\n", err)
		}
	}
	return string(output), err
}

func (e *nativeExecutor) ExecBash(script string) (string, error) {
	// Replace ~ with testHome
	script = strings.ReplaceAll(script, "~/", e.testHome+"/")
	script = strings.ReplaceAll(script, "$HOME", e.testHome)

	cmd := exec.Command("bash", "-c", script)
	cmd.Env = e.buildEnv()
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func (e *nativeExecutor) buildEnv() []string {
	env := append(os.Environ(), "HOME="+e.testHome)
	for k, v := range e.envVars {
		env = append(env, k+"="+v)
	}
	return env
}

func (e *nativeExecutor) Setup() error {
	var err error
	e.testHome, err = os.MkdirTemp("", "tomei-e2e-")
	if err != nil {
		return fmt.Errorf("failed to create temp home: %w", err)
	}

	// Create necessary directory structure
	dirs := []string{
		filepath.Join(e.testHome, ".local", "bin"),
		filepath.Join(e.testHome, ".local", "share", "tomei", "tools"),
		filepath.Join(e.testHome, ".local", "share", "tomei", "runtimes"),
		filepath.Join(e.testHome, ".config", "tomei"),
		filepath.Join(e.testHome, "go", "bin"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Copy test config files from e2e/config to testHome
	if err := e.copyTestConfigs(); err != nil {
		return fmt.Errorf("failed to copy test configs: %w", err)
	}

	return nil
}

func (e *nativeExecutor) copyTestConfigs() error {
	// Find e2e/config directory relative to test file location
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("failed to get current file path")
	}
	e2eDir := filepath.Dir(filename)
	configDir := filepath.Join(e2eDir, "config")

	// Copy manifests
	if err := copyDir(filepath.Join(configDir, "manifests"), filepath.Join(e.testHome, "manifests")); err != nil {
		return fmt.Errorf("failed to copy manifests: %w", err)
	}

	// Copy registry configs to manifests/registry
	if err := copyDir(filepath.Join(configDir, "registry"), filepath.Join(e.testHome, "manifests", "registry")); err != nil {
		return fmt.Errorf("failed to copy registry: %w", err)
	}

	// Copy delegation-test configs
	if err := copyDir(filepath.Join(configDir, "delegation-test"), filepath.Join(e.testHome, "delegation-test")); err != nil {
		return fmt.Errorf("failed to copy delegation-test: %w", err)
	}

	// Copy dependency-test configs
	if err := copyDir(filepath.Join(configDir, "dependency-test"), filepath.Join(e.testHome, "dependency-test")); err != nil {
		return fmt.Errorf("failed to copy dependency-test: %w", err)
	}

	// Copy installer-repo-test configs
	if err := copyDir(filepath.Join(configDir, "installer-repo-test"), filepath.Join(e.testHome, "installer-repo-test")); err != nil {
		return fmt.Errorf("failed to copy installer-repo-test: %w", err)
	}

	// Copy logs-test configs
	if err := copyDir(filepath.Join(configDir, "logs-test"), filepath.Join(e.testHome, "logs-test")); err != nil {
		return fmt.Errorf("failed to copy logs-test: %w", err)
	}

	return nil
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return err
			}
			if err := os.WriteFile(dstPath, data, 0644); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *nativeExecutor) Cleanup() error {
	if e.testHome != "" {
		return os.RemoveAll(e.testHome)
	}
	return nil
}

func (e *nativeExecutor) Setenv(key, value string) {
	if e.envVars == nil {
		e.envVars = make(map[string]string)
	}
	e.envVars[key] = value
}

func (e *nativeExecutor) Getenv(key string) string {
	if e.envVars == nil {
		return ""
	}
	return e.envVars[key]
}

// newExecutor creates an executor based on environment variables.
//
// Environment variables:
//   - TOMEI_E2E_CONTAINER: Container name for container mode
//   - TOMEI_E2E_NATIVE: Set to "true" for native mode
//   - TOMEI_E2E_BINARY: Path to tomei binary (native mode, optional)
func newExecutor() (executor, error) {
	// 1. Container mode (TOMEI_E2E_CONTAINER is set)
	if container := os.Getenv("TOMEI_E2E_CONTAINER"); container != "" {
		return &containerExecutor{containerName: container}, nil
	}

	// 2. Native mode (TOMEI_E2E_NATIVE is set)
	if os.Getenv("TOMEI_E2E_NATIVE") == "true" {
		binary := os.Getenv("TOMEI_E2E_BINARY")
		if binary == "" {
			// Look for tomei in PATH
			var err error
			binary, err = exec.LookPath("tomei")
			if err != nil {
				return nil, fmt.Errorf("tomei binary not found in PATH, set TOMEI_E2E_BINARY")
			}
		}
		return &nativeExecutor{tomeiBinary: binary}, nil
	}

	// 3. Neither mode is set
	return nil, fmt.Errorf("set TOMEI_E2E_CONTAINER for container mode, or TOMEI_E2E_NATIVE=true for native mode")
}

// testExec is the global executor instance used by all tests
var testExec executor

// targetArch is the target architecture for tests
var targetArch string

func init() {
	// Get target architecture from GOARCH env var, default to host architecture
	targetArch = os.Getenv("GOARCH")
	if targetArch == "" {
		targetArch = runtime.GOARCH
	}
}
