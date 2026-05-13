package apt

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/installer/command"
	"github.com/terassyi/tomei/internal/resource"
)

func TestParseAptVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		output  string
		want    string
		wantErr bool
	}{
		{
			name:   "standard version",
			output: "apt 2.4.12 (amd64)",
			want:   "2.4.12",
		},
		{
			name:   "version with build suffix",
			output: "apt 2.7.14build2 (amd64)",
			want:   "2.7.14build2",
		},
		{
			name:   "multiline output",
			output: "apt 2.4.12 (amd64)\nUsage: apt-get [options] command\n",
			want:   "2.4.12",
		},
		{
			name:    "empty output",
			output:  "",
			wantErr: true,
		},
		{
			name:    "single word",
			output:  "apt",
			wantErr: true,
		},
		{
			name:    "whitespace only",
			output:  "   \n  ",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseAptVersion(tt.output)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// --- mock ---

// mockCommandRunner is the package-local CommandRunner stub used across
// every apt unit test. It records each call and can return per-call
// outputs and errors, so multi-step flows (e.g. PackageRepositoryInstaller
// which fires 4–6 shell calls in a single Install) can assert the exact
// sequence as well as inject failures at specific steps.
//
// Backward-compat for the single-call helpers (IsInstalled, PackageVersion,
// Update, PackageSetInstaller.Install/Remove):
//
//   - captureCmds is the flat concatenation of every cmds slice passed in
//     (1-element-per-call in current callers). Existing assertions like
//     require.Len(t, captureCmds, 1) + captureCmds[0] continue to work
//     unchanged because each helper still makes exactly one call.
//   - captureOutput / captureErr are returned for every call unless the
//     captureOutputs / captureErrs sequence has an entry at the call index.
//
// Sequence mode (for multi-call flows):
//
//   - captureCallCmds[i] is the cmds slice passed on the i-th call.
//   - captureOutputs[i] / captureErrs[i] override captureOutput /
//     captureErr for the i-th call, otherwise the legacy field is used.
//     Pass shorter sequences and the legacy field acts as the default for
//     calls past len(sequence).
type mockCommandRunner struct {
	// captureCmds is the flat list of every cmd across every call.
	captureCmds []string
	// captureCallCmds[i] is the cmds slice passed on the i-th call.
	captureCallCmds [][]string
	// captureMethods[i] is "capture" or "withoutput" for the i-th call.
	captureMethods []string
	// captureOutput is the default ExecuteCapture return when captureOutputs
	// has no entry at the call index.
	captureOutput string
	// captureOutputs, when non-empty, returns the i-th element for the
	// i-th ExecuteCapture call.
	captureOutputs []string
	// captureErr is the default error returned when captureErrs has no
	// entry at the call index.
	captureErr error
	// captureErrs, when non-empty, returns the i-th element for the i-th
	// call regardless of method (ExecuteCapture or ExecuteWithOutput).
	captureErrs []error
}

var _ CommandRunner = (*mockCommandRunner)(nil)

// record stores cmds and returns (output, err) for the current call,
// preferring sequence-mode fields over the legacy single-value defaults.
func (m *mockCommandRunner) record(method string, cmds []string) (string, error) {
	idx := len(m.captureCallCmds)
	m.captureCmds = append(m.captureCmds, cmds...)
	m.captureCallCmds = append(m.captureCallCmds, cmds)
	m.captureMethods = append(m.captureMethods, method)

	out := m.captureOutput
	if idx < len(m.captureOutputs) {
		out = m.captureOutputs[idx]
	}
	err := m.captureErr
	if idx < len(m.captureErrs) {
		err = m.captureErrs[idx]
	}
	return out, err
}

func (m *mockCommandRunner) ExecuteCapture(_ context.Context, cmds []string, _ command.Vars, _ map[string]string) (string, error) {
	return m.record("capture", cmds)
}

func (m *mockCommandRunner) ExecuteWithOutput(_ context.Context, cmds []string, _ command.Vars, _ map[string]string, _ command.OutputCallback) error {
	_, err := m.record("withoutput", cmds)
	return err
}

// --- VersionFunc tests ---

func TestVersionFunc_Success(t *testing.T) {
	t.Parallel()
	mock := &mockCommandRunner{captureOutput: "apt 2.7.14build2 (amd64)"}
	vf := New(mock).VersionFunc()

	version, err := vf(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "2.7.14build2", version)
}

func TestVersionFunc_CommandError(t *testing.T) {
	t.Parallel()
	mock := &mockCommandRunner{captureErr: fmt.Errorf("exec: \"apt-get\": executable file not found in $PATH")}
	vf := New(mock).VersionFunc()

	_, err := vf(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to run apt-get --version")
}

func TestVersionFunc_ParseError(t *testing.T) {
	t.Parallel()
	mock := &mockCommandRunner{captureOutput: "apt"}
	vf := New(mock).VersionFunc()

	_, err := vf(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected apt-get --version output")
}

// --- Update tests ---

func TestUpdate_Success(t *testing.T) {
	t.Parallel()
	runner := &mockCommandRunner{}
	err := New(runner).Update(context.Background())
	require.NoError(t, err)
	require.Len(t, runner.captureCmds, 1)
	assert.Equal(t,
		"sudo -n env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C apt-get update",
		runner.captureCmds[0],
	)
}

func TestUpdate_CommandError(t *testing.T) {
	t.Parallel()
	runner := &mockCommandRunner{captureErr: errors.New("exit status 100")}
	err := New(runner).Update(context.Background())
	require.Error(t, err)
	// Pin the wrap structure (prefix + delimiter + wrapped cause) so a
	// future rename of the wrap format is caught here, not just the
	// substring "apt: update".
	assert.EqualError(t, err, "apt: update: exit status 100")
}

// --- PackageSetInstaller tests ---

func TestPackageSetInstaller_Install(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		packages  []string
		runnerErr error
		wantErr   string
		wantCmd   string
	}{
		{
			name:     "single package",
			packages: []string{"git"},
			wantCmd:  "sudo -n env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C apt-get install -y -o DPkg::Lock::Timeout=60 -- git",
		},
		{
			name:     "multiple packages",
			packages: []string{"git", "curl", "tree"},
			wantCmd:  "sudo -n env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C apt-get install -y -o DPkg::Lock::Timeout=60 -- git curl tree",
		},
		{
			name:     "empty packages",
			packages: []string{},
			wantErr:  "apt: install requires at least one package",
		},
		{
			name:     "empty string in packages slice",
			packages: []string{""},
			wantErr:  "apt: empty package name in install list",
		},
		{
			name:     "empty string among valid packages",
			packages: []string{"git", "", "tree"},
			wantErr:  "apt: empty package name in install list",
		},
		{
			name:     "package with semicolon rejected",
			packages: []string{"git;curl evil|sh"},
			wantErr:  "contains disallowed characters",
		},
		{
			name:     "package with backtick rejected",
			packages: []string{"git", "tree`whoami`"},
			wantErr:  "contains disallowed characters",
		},
		{
			name:     "package with embedded space rejected",
			packages: []string{"git vim"},
			wantErr:  "contains disallowed characters",
		},
		{
			name:     "package with newline rejected",
			packages: []string{"git\n"},
			wantErr:  "contains disallowed characters",
		},
		{
			name:     "package with glob star rejected",
			packages: []string{"linux-image-*"},
			wantErr:  "contains disallowed characters",
		},
		{
			name:      "runner error wraps packages context",
			packages:  []string{"nonexistent-pkg"},
			runnerErr: errors.New("exit status 100"),
			wantErr:   `apt: install ["nonexistent-pkg"]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner := &mockCommandRunner{captureErr: tt.runnerErr}
			res := &resource.SystemPackageSet{
				SystemPackageSetSpec: &resource.SystemPackageSetSpec{
					InstallerRef: "apt",
					Packages:     tt.packages,
				},
			}
			state, err := New(runner).PackageSetInstaller().Install(context.Background(), res, "cli-tools")
			if tt.wantErr == "" {
				require.NoError(t, err)
				require.Len(t, runner.captureCmds, 1)
				assert.Equal(t, tt.wantCmd, runner.captureCmds[0])
				require.NotNil(t, state)
				assert.Equal(t, "apt", state.InstallerRef)
				assert.Equal(t, tt.packages, state.Packages)
				assert.NotNil(t, state.InstalledVersions)
				assert.False(t, state.UpdatedAt.IsZero(), "UpdatedAt should be set")
			} else {
				require.Error(t, err)
				assert.Nil(t, state)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestPackageSetInstaller_Install_PropagatesRepositoryRef(t *testing.T) {
	t.Parallel()
	runner := &mockCommandRunner{}
	res := &resource.SystemPackageSet{
		SystemPackageSetSpec: &resource.SystemPackageSetSpec{
			InstallerRef:  "apt",
			RepositoryRef: "docker",
			Packages:      []string{"docker-ce"},
		},
	}
	state, err := New(runner).PackageSetInstaller().Install(context.Background(), res, "docker")
	require.NoError(t, err)
	assert.Equal(t, "docker", state.RepositoryRef)
}

func TestPackageSetInstaller_Remove(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		packages  []string
		runnerErr error
		wantErr   string
		wantCmd   string
	}{
		{
			name:     "single package",
			packages: []string{"git"},
			wantCmd:  "sudo -n env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C apt-get remove -y -o DPkg::Lock::Timeout=60 -- git",
		},
		{
			name:     "multiple packages",
			packages: []string{"git", "curl", "tree"},
			wantCmd:  "sudo -n env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C apt-get remove -y -o DPkg::Lock::Timeout=60 -- git curl tree",
		},
		{
			name:     "empty packages is a no-op",
			packages: []string{},
			// No error, no shell command issued — Remove short-circuits
			// so the executor can still delete the state file.
		},
		{
			name:     "empty string in packages slice",
			packages: []string{""},
			wantErr:  "apt: empty package name in remove list",
		},
		{
			name:     "empty string among valid packages",
			packages: []string{"git", "", "tree"},
			wantErr:  "apt: empty package name in remove list",
		},
		{
			name:     "package with semicolon rejected",
			packages: []string{"git;curl evil|sh"},
			wantErr:  "contains disallowed characters",
		},
		{
			name:     "package with backtick rejected",
			packages: []string{"git", "tree`whoami`"},
			wantErr:  "contains disallowed characters",
		},
		{
			name:     "package with embedded space rejected",
			packages: []string{"git vim"},
			wantErr:  "contains disallowed characters",
		},
		{
			name:     "package with newline rejected",
			packages: []string{"git\n"},
			wantErr:  "contains disallowed characters",
		},
		{
			name:     "package with glob star rejected",
			packages: []string{"linux-image-*"},
			wantErr:  "contains disallowed characters",
		},
		{
			name:      "runner error wraps packages context",
			packages:  []string{"nonexistent-pkg"},
			runnerErr: errors.New("exit status 100"),
			wantErr:   `apt: remove ["nonexistent-pkg"]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner := &mockCommandRunner{captureErr: tt.runnerErr}
			state := &resource.SystemPackageSetState{
				InstallerRef: "apt",
				Packages:     tt.packages,
			}
			err := New(runner).PackageSetInstaller().Remove(context.Background(), state, "cli-tools")
			switch {
			case tt.wantErr != "":
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			case tt.wantCmd != "":
				require.NoError(t, err)
				require.Len(t, runner.captureCmds, 1)
				assert.Equal(t, tt.wantCmd, runner.captureCmds[0])
			default:
				// No-op: success without issuing any shell command.
				require.NoError(t, err)
				assert.Empty(t, runner.captureCmds)
			}
		})
	}
}

func TestPackageSetInstaller_Remove_NilState(t *testing.T) {
	t.Parallel()
	runner := &mockCommandRunner{}
	err := New(runner).PackageSetInstaller().Remove(context.Background(), nil, "cli-tools")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil state")
	assert.Empty(t, runner.captureCmds)
}

// realExitError builds a real *exec.ExitError via `sh -c 'exit N'`.
// Skips the calling test when POSIX sh is unavailable (Windows, minimal
// images without sh) so `go test ./...` is portable. Duplicated from
// internal/installer/command/exit_test.go because Go's _test.go scoping
// prevents cross-package test-helper sharing.
func realExitError(t *testing.T, code int) error {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("realExitError requires POSIX sh; not available on Windows")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not found in PATH")
	}
	err := exec.Command("sh", "-c", fmt.Sprintf("exit %d", code)).Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	return err
}

// wrapRunnerErr mimics the wrap shape that command.Executor.ExecuteCapture
// applies to a non-zero subprocess exit so errors.As walks the chain in
// the same way as production. Pinning the format here in one place catches
// drift in ExecuteCapture's wrap.
func wrapRunnerErr(pkg string, cause error) error {
	return fmt.Errorf(`command failed: dpkg-query -W -f='${db:Status-Status}\n' -- %s: %w`, pkg, cause)
}

// --- IsInstalled tests ---

func TestIsInstalled(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		pkg       string
		runnerOut string
		runnerErr error
		want      bool
		wantErr   string
	}{
		// installed states
		{name: "installed", pkg: "bash", runnerOut: "installed\n", want: true},
		{name: "installed without trailing newline", pkg: "bash", runnerOut: "installed", want: true},
		{name: "installed with surrounding whitespace", pkg: "bash", runnerOut: "  installed  \n", want: true},
		// not-installed states (strict)
		{name: "config-files (rc) treated as not installed", pkg: "vim", runnerOut: "config-files\n", want: false},
		{name: "not-installed", pkg: "ghost", runnerOut: "not-installed\n", want: false},
		{name: "half-installed treated as not installed", pkg: "broken", runnerOut: "half-installed\n", want: false},
		{name: "half-configured treated as not installed", pkg: "broken", runnerOut: "half-configured\n", want: false},
		// multi-arch
		{name: "multi-arch with one installed", pkg: "libc6", runnerOut: "installed\nconfig-files\n", want: true},
		{name: "multi-arch with both installed", pkg: "libc6", runnerOut: "installed\ninstalled\n", want: true},
		{name: "multi-arch with none installed", pkg: "libc6", runnerOut: "config-files\nnot-installed\n", want: false},
		{name: "multi-arch suffix syntax", pkg: "libc6:amd64", runnerOut: "installed\n", want: true},
		// boundary-of-allowed pkg names
		{name: "hyphen in pkg name", pkg: "linux-image-amd64", runnerOut: "installed\n", want: true},
		{name: "plus in pkg name", pkg: "g++", runnerOut: "installed\n", want: true},
		{name: "dot in pkg name", pkg: "python3.10", runnerOut: "installed\n", want: true},
		// degenerate / edge outputs
		{name: "empty output", pkg: "ghost", runnerOut: "", want: false},
		{name: "newline-only output", pkg: "ghost", runnerOut: "\n\n", want: false},
		{name: "sub-string non-match (installed-extra)", pkg: "broken", runnerOut: "installed-extra\n", want: false},
		{name: "trailing garbage after installed", pkg: "broken", runnerOut: "installedXXX\n", want: false},
		// validation
		{name: "empty package name rejected", pkg: "", wantErr: "apt: empty package name"},
		{name: "package with semicolon rejected", pkg: "bash; rm -rf /", wantErr: "contains disallowed characters"},
		{name: "package with backtick rejected", pkg: "bash`whoami`", wantErr: "contains disallowed characters"},
		{name: "package with embedded space rejected", pkg: "bash vim", wantErr: "contains disallowed characters"},
		{name: "package with newline rejected", pkg: "bash\n", wantErr: "contains disallowed characters"},
		{name: "package with glob star rejected", pkg: "linux-image-*", wantErr: "contains disallowed characters"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner := &mockCommandRunner{captureOutput: tt.runnerOut, captureErr: tt.runnerErr}
			got, err := New(runner).IsInstalled(context.Background(), tt.pkg)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.False(t, got)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			require.Len(t, runner.captureCmds, 1)
			assert.Equal(t,
				`dpkg-query -W -f='${db:Status-Status}\n' -- `+tt.pkg,
				runner.captureCmds[0],
			)
		})
	}
}

// TestIsInstalled_UnknownPackageExit1 verifies exit 1 (unknown package)
// is mapped to (false, nil) using a real *exec.ExitError, exercising
// the errors.As chain through command.Executor's `command failed: %s: %w` wrap.
func TestIsInstalled_UnknownPackageExit1(t *testing.T) {
	t.Parallel()
	wrapped := wrapRunnerErr("ghost-pkg", realExitError(t, 1))
	runner := &mockCommandRunner{captureErr: wrapped}

	got, err := New(runner).IsInstalled(context.Background(), "ghost-pkg")
	require.NoError(t, err)
	assert.False(t, got)
}

// TestIsInstalled_GenuineFailureExit2 verifies non-1 exit codes propagate
// with exactly `apt: status "<pkg>": <inner>`. EqualError pin (instead of
// Contains) catches accidental rewording — the format is part of the
// public contract per #207's precedent.
func TestIsInstalled_GenuineFailureExit2(t *testing.T) {
	t.Parallel()
	exitErr := realExitError(t, 2)
	wrapped := wrapRunnerErr("bash", exitErr)
	runner := &mockCommandRunner{captureErr: wrapped}

	got, err := New(runner).IsInstalled(context.Background(), "bash")
	require.Error(t, err)
	assert.False(t, got)
	require.EqualError(t, err, fmt.Sprintf(`apt: status "bash": %s`, wrapped.Error()))
	require.ErrorIs(t, err, exitErr)
}

// TestIsInstalled_NonExitErrorPropagates verifies runner errors that are
// not *exec.ExitError (binary not found, ctx canceled, permission denied)
// bypass the exit-1 special case.
func TestIsInstalled_NonExitErrorPropagates(t *testing.T) {
	t.Parallel()
	sentinel := errors.New(`exec: "dpkg-query": executable file not found in $PATH`)
	runner := &mockCommandRunner{captureErr: sentinel}

	got, err := New(runner).IsInstalled(context.Background(), "bash")
	require.Error(t, err)
	assert.False(t, got)
	require.EqualError(t, err, `apt: status "bash": exec: "dpkg-query": executable file not found in $PATH`)
	require.ErrorIs(t, err, sentinel)
}

// TestIsInstalled_CtxCanceledTakesPriority verifies that when ctx is
// canceled the cancellation reason is preserved in the chain even if
// the runner returns an exit-1 *exec.ExitError that would normally be
// mapped to (false, nil). Without the ctx.Err() pre-check at the top
// of the err branch, cancellations would silently surface as
// "package not installed", which is a wrong-answer bug for callers
// that intend to retry on cancellation.
func TestIsInstalled_CtxCanceledTakesPriority(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := &mockCommandRunner{captureErr: wrapRunnerErr("bash", realExitError(t, 1))}

	got, err := New(runner).IsInstalled(ctx, "bash")
	require.Error(t, err)
	assert.False(t, got)
	require.ErrorIs(t, err, context.Canceled)
}

// wrapVersionRunnerErr mimics command.Executor.ExecuteCapture's wrap
// shape for the PackageVersion command. Pinned in one place to catch
// drift in production wrap shape.
func wrapVersionRunnerErr(pkg string, cause error) error {
	return fmt.Errorf(`command failed: dpkg-query -W -f='${db:Status-Status} ${Version}\n' -- %s: %w`, pkg, cause)
}

// --- PackageVersion tests ---

func TestPackageVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		pkg       string
		runnerOut string
		runnerErr error
		want      string
		wantErr   string
	}{
		// success — simple
		{name: "simple version", pkg: "bash", runnerOut: "installed 5.1-6ubuntu1\n", want: "5.1-6ubuntu1"},
		{name: "epoch version", pkg: "vim", runnerOut: "installed 2:8.2.3995-1ubuntu2.13\n", want: "2:8.2.3995-1ubuntu2.13"},
		{name: "ubuntu suffix", pkg: "git", runnerOut: "installed 1:2.34.1-1ubuntu1.10\n", want: "1:2.34.1-1ubuntu1.10"},
		{name: "no trailing newline", pkg: "bash", runnerOut: "installed 5.1-6ubuntu1", want: "5.1-6ubuntu1"},
		{name: "extra whitespace tolerated", pkg: "bash", runnerOut: "  installed   5.1-6ubuntu1  \n", want: "5.1-6ubuntu1"},

		// success — multi-arch with only one installed (others non-installed)
		{name: "one installed + one config-files", pkg: "libc6", runnerOut: "installed 2.35-0ubuntu3.5\nconfig-files 2.35-0ubuntu3.5\n", want: "2.35-0ubuntu3.5"},
		{name: "one installed + one not-installed", pkg: "libc6", runnerOut: "installed 2.35-0ubuntu3.5\nnot-installed \n", want: "2.35-0ubuntu3.5"},
		{name: "multi-arch suffix syntax (single match)", pkg: "libc6:amd64", runnerOut: "installed 2.35-0ubuntu3.5\n", want: "2.35-0ubuntu3.5"},

		// success — boundary-of-allowed pkg names
		{name: "hyphen pkg name", pkg: "linux-image-amd64", runnerOut: "installed 5.15.0.1\n", want: "5.15.0.1"},
		{name: "plus pkg name", pkg: "g++", runnerOut: "installed 4:11.2.0-1ubuntu1\n", want: "4:11.2.0-1ubuntu1"},
		{name: "dot pkg name", pkg: "python3.10", runnerOut: "installed 3.10.6-1~22.04\n", want: "3.10.6-1~22.04"},

		// not-installed — exit 0 + 0 installed lines (stale-version protection)
		{name: "config-files only treated as not installed", pkg: "vim", runnerOut: "config-files 8.2.0\n", wantErr: `apt: package "vim" is not installed`},
		{name: "not-installed only treated as not installed", pkg: "ghost", runnerOut: "not-installed \n", wantErr: `apt: package "ghost" is not installed`},
		{name: "half-installed treated as not installed", pkg: "broken", runnerOut: "half-installed 1.0\n", wantErr: `apt: package "broken" is not installed`},
		{name: "half-configured treated as not installed", pkg: "broken", runnerOut: "half-configured 1.0\n", wantErr: `apt: package "broken" is not installed`},
		{name: "multi-arch all config-files", pkg: "libc6", runnerOut: "config-files 2.35-0ubuntu3.5\nconfig-files 2.35-0ubuntu3.5\n", wantErr: `apt: package "libc6" is not installed`},

		// multi-arch ambiguity — exit 0 + 2+ installed lines
		{name: "multi-arch both installed (same version)", pkg: "libc6", runnerOut: "installed 2.35-0ubuntu3.5\ninstalled 2.35-0ubuntu3.5\n", wantErr: `apt: package "libc6" is installed for multiple architectures`},
		{name: "multi-arch both installed (different versions)", pkg: "libc6", runnerOut: "installed 2.35-0ubuntu3.5\ninstalled 2.34-0ubuntu3.4\n", wantErr: `apt: package "libc6" is installed for multiple architectures`},

		// degenerate / edge outputs
		{name: "empty output", pkg: "ghost", runnerOut: "", wantErr: `apt: package "ghost" is not installed`},
		{name: "newline-only output", pkg: "ghost", runnerOut: "\n\n", wantErr: `apt: package "ghost" is not installed`},
		{name: "installed without version field", pkg: "broken", runnerOut: "installed\n", wantErr: `apt: package "broken" is not installed`},

		// validation
		{name: "empty pkg name rejected", pkg: "", wantErr: "apt: empty package name"},
		{name: "pkg with semicolon rejected", pkg: "bash; rm -rf /", wantErr: "contains disallowed characters"},
		{name: "pkg with backtick rejected", pkg: "bash`whoami`", wantErr: "contains disallowed characters"},
		{name: "pkg with embedded space rejected", pkg: "bash vim", wantErr: "contains disallowed characters"},
		{name: "pkg with newline rejected", pkg: "bash\n", wantErr: "contains disallowed characters"},
		{name: "pkg with glob star rejected", pkg: "linux-image-*", wantErr: "contains disallowed characters"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner := &mockCommandRunner{captureOutput: tt.runnerOut, captureErr: tt.runnerErr}
			got, err := New(runner).PackageVersion(context.Background(), tt.pkg)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Empty(t, got)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			require.Len(t, runner.captureCmds, 1)
			assert.Equal(t,
				`dpkg-query -W -f='${db:Status-Status} ${Version}\n' -- `+tt.pkg,
				runner.captureCmds[0],
			)
		})
	}
}

// TestPackageVersion_NotInstalledExit1 verifies exit 1 (unknown package)
// is mapped to a wrapped "is not installed" error using a real
// *exec.ExitError, exercising the errors.As chain through
// command.Executor's `command failed: %s: %w` wrap.
func TestPackageVersion_NotInstalledExit1(t *testing.T) {
	t.Parallel()
	exitErr := realExitError(t, 1)
	wrapped := wrapVersionRunnerErr("ghost-pkg", exitErr)
	runner := &mockCommandRunner{captureErr: wrapped}

	got, err := New(runner).PackageVersion(context.Background(), "ghost-pkg")
	require.Error(t, err)
	assert.Empty(t, got)
	require.EqualError(t, err, fmt.Sprintf(`apt: package "ghost-pkg" is not installed: %s`, wrapped.Error()))
	require.ErrorIs(t, err, exitErr)
}

// TestPackageVersion_GenuineFailureExit2 verifies non-1 exit codes
// propagate with exactly `apt: version "<pkg>": <inner>`. EqualError
// pin (instead of Contains) catches accidental rewording — the format
// is part of the public contract per #207's precedent.
func TestPackageVersion_GenuineFailureExit2(t *testing.T) {
	t.Parallel()
	exitErr := realExitError(t, 2)
	wrapped := wrapVersionRunnerErr("bash", exitErr)
	runner := &mockCommandRunner{captureErr: wrapped}

	got, err := New(runner).PackageVersion(context.Background(), "bash")
	require.Error(t, err)
	assert.Empty(t, got)
	require.EqualError(t, err, fmt.Sprintf(`apt: version "bash": %s`, wrapped.Error()))
	require.ErrorIs(t, err, exitErr)
}

// TestPackageVersion_NonExitErrorPropagates verifies runner errors that
// are not *exec.ExitError (binary not found, ctx canceled, permission
// denied) bypass the exit-1 special case.
func TestPackageVersion_NonExitErrorPropagates(t *testing.T) {
	t.Parallel()
	sentinel := errors.New(`exec: "dpkg-query": executable file not found in $PATH`)
	runner := &mockCommandRunner{captureErr: sentinel}

	got, err := New(runner).PackageVersion(context.Background(), "bash")
	require.Error(t, err)
	assert.Empty(t, got)
	require.EqualError(t, err, `apt: version "bash": exec: "dpkg-query": executable file not found in $PATH`)
	require.ErrorIs(t, err, sentinel)
}

// TestPackageVersion_CtxCanceledTakesPriority verifies that when ctx is
// canceled the cancellation reason is preserved in the chain even if
// the runner returns an exit-1 *exec.ExitError that would normally be
// wrapped as "is not installed". Without the ctx.Err() pre-check at
// the top of the err branch, cancellations would be misclassified as
// "not installed", which is a wrong-answer bug for callers that intend
// to retry on cancellation.
func TestPackageVersion_CtxCanceledTakesPriority(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := &mockCommandRunner{captureErr: wrapVersionRunnerErr("bash", realExitError(t, 1))}

	got, err := New(runner).PackageVersion(ctx, "bash")
	require.Error(t, err)
	assert.Empty(t, got)
	require.ErrorIs(t, err, context.Canceled)
}
