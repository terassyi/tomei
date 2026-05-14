package apt

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/installer/command"
	"github.com/terassyi/tomei/internal/installer/executor"
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
	// captureOutput is the default return when captureOutputs has no
	// entry at the call index. For ExecuteCapture it is the returned
	// string; for ExecuteWithOutput it is the canned text fed back
	// through the callback line-by-line so streaming consumers can be
	// exercised from tests.
	captureOutput string
	// captureOutputs, when non-empty, returns the i-th element on the
	// i-th call regardless of method (ExecuteCapture or
	// ExecuteWithOutput). Indexed by overall call sequence, not per-
	// method; pass shorter sequences and captureOutput acts as the
	// default for calls past len(captureOutputs).
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

func (m *mockCommandRunner) ExecuteWithOutput(_ context.Context, cmds []string, _ command.Vars, _ map[string]string, callback command.OutputCallback) error {
	out, err := m.record("withoutput", cmds)
	// Replay the recorded output as line-by-line callback invocations
	// so tests that exercise streaming consumers (e.g. the apt-get
	// update partial-fetch collector) can inject canned output via
	// captureOutputs and have it observed by the production callback.
	// bufio.Scanner mirrors the real command.Executor.streamOutput
	// implementation — strips trailing line terminators and (critically)
	// does NOT emit a final empty token when the input ends with a
	// newline, unlike strings.SplitSeq.
	if callback != nil && out != "" {
		scanner := bufio.NewScanner(strings.NewReader(out))
		for scanner.Scan() {
			callback(scanner.Text())
		}
	}
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
	// Runner call sequence:
	//
	//	indices 0..N-1: pre-install IsInstalled probes (one per package).
	//	                Empty output ⇒ not installed (the bottom of
	//	                IsInstalled returns false, nil when no status line
	//	                matches "installed"). Used to snapshot which
	//	                packages this Install action newly placed for the
	//	                rollback differential.
	//	index   N    : apt-get install (no output read).
	//	indices N+1..2N: post-install dpkg-query version probes (one per
	//	                package, in spec order). probeOutputs[i] is the
	//	                output returned for the i-th version probe.
	tests := []struct {
		name         string
		packages     []string
		probeOutputs []string
		runnerErr    error
		wantErr      string
		wantCmds     []string
		wantVersions map[string]string
	}{
		{
			name:         "single package",
			packages:     []string{"git"},
			probeOutputs: []string{"installed 1:2.34.1-1ubuntu1.10\n"},
			wantCmds: []string{
				`dpkg-query -W -f='${db:Status-Status}\n' -- git`,
				"sudo -n env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C apt-get install -y -o DPkg::Lock::Timeout=60 -- git",
				`dpkg-query -W -f='${db:Status-Status} ${Version}\n' -- git`,
			},
			wantVersions: map[string]string{"git": "1:2.34.1-1ubuntu1.10"},
		},
		{
			name:     "multiple packages",
			packages: []string{"git", "curl", "tree"},
			probeOutputs: []string{
				"installed 1:2.34.1-1ubuntu1.10\n",
				"installed 7.81.0-1ubuntu1.13\n",
				"installed 1.8.0-5build1\n",
			},
			wantCmds: []string{
				`dpkg-query -W -f='${db:Status-Status}\n' -- git`,
				`dpkg-query -W -f='${db:Status-Status}\n' -- curl`,
				`dpkg-query -W -f='${db:Status-Status}\n' -- tree`,
				"sudo -n env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C apt-get install -y -o DPkg::Lock::Timeout=60 -- git curl tree",
				`dpkg-query -W -f='${db:Status-Status} ${Version}\n' -- git`,
				`dpkg-query -W -f='${db:Status-Status} ${Version}\n' -- curl`,
				`dpkg-query -W -f='${db:Status-Status} ${Version}\n' -- tree`,
			},
			wantVersions: map[string]string{
				"git":  "1:2.34.1-1ubuntu1.10",
				"curl": "7.81.0-1ubuntu1.13",
				"tree": "1.8.0-5build1",
			},
		},
		{
			name:     "empty packages",
			packages: []string{},
			// spec.Validate() catches this at the apt boundary before
			// runInstall, producing the package-set-scoped wrap.
			wantErr: `apt: package set "cli-tools": at least one package is required`,
		},
		{
			name:     "empty string in packages slice",
			packages: []string{""},
			wantErr:  `apt: package set "cli-tools": packages[0] must not be empty`,
		},
		{
			name:     "empty string among valid packages",
			packages: []string{"git", "", "tree"},
			wantErr:  `apt: package set "cli-tools": packages[1] must not be empty`,
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
			// Pre-install IsInstalled probe fires first and is the call
			// that surfaces the runner error. The probe wrap is
			// "apt: status %q" (from Client.IsInstalled) which is then
			// scoped by Install as "apt: package set %q: snapshot
			// pre-install state of %q: <probe wrap>: <runner err>".
			wantErr: `apt: package set "cli-tools": snapshot pre-install state of "nonexistent-pkg"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Pre-install IsInstalled probes (N) + apt-get install (1) all
			// consume default "" (empty output) — IsInstalled treats no
			// "installed" line as "not installed", apt-get install does
			// not have its output read. probeOutputs feeds the version
			// probes starting at index N+1.
			captureOutputs := append(make([]string, len(tt.packages)+1), tt.probeOutputs...)
			runner := &mockCommandRunner{
				captureErr:     tt.runnerErr,
				captureOutputs: captureOutputs,
			}
			res := &resource.SystemPackageSet{
				SystemPackageSetSpec: &resource.SystemPackageSetSpec{
					InstallerRef: "apt",
					Packages:     tt.packages,
				},
			}
			state, err := New(runner).PackageSetInstaller().Install(context.Background(), res, "cli-tools")
			if tt.wantErr == "" {
				require.NoError(t, err)
				assert.Equal(t, tt.wantCmds, runner.captureCmds)
				require.NotNil(t, state)
				assert.Equal(t, "apt", state.InstallerRef)
				assert.Equal(t, tt.packages, state.Packages)
				assert.Equal(t, tt.wantVersions, state.InstalledVersions)
				assert.False(t, state.UpdatedAt.IsZero(), "UpdatedAt should be set")
			} else {
				require.Error(t, err)
				assert.Nil(t, state)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestPackageSetInstaller_Install_NilGuards(t *testing.T) {
	t.Parallel()
	// Defense-in-depth at the apt boundary. Mirrors PackageRepository
	// installer's `apt: repository %q: nil spec` shape (repository.go:394).
	cases := []struct {
		name string
		res  *resource.SystemPackageSet
	}{
		{"nil resource", nil},
		{"nil spec", &resource.SystemPackageSet{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runner := &mockCommandRunner{}
			state, err := New(runner).PackageSetInstaller().Install(context.Background(), tc.res, "cli-tools")
			require.Error(t, err)
			assert.Nil(t, state)
			assert.Contains(t, err.Error(), `apt: package set "cli-tools": nil spec`)
			assert.Empty(t, runner.captureCmds, "apt-get install must not run when spec is missing")
		})
	}
}

func TestPackageSetInstaller_Install_PropagatesRepositoryRef(t *testing.T) {
	t.Parallel()
	// Sequence:
	//   index 0 = pre-install IsInstalled probe for "docker-ce" (empty
	//             output ⇒ not installed).
	//   index 1 = apt-get install (no output read).
	//   index 2 = dpkg-query version probe for "docker-ce".
	runner := &mockCommandRunner{
		captureOutputs: []string{"", "", "installed 5:24.0.7-1~ubuntu.22.04~jammy\n"},
	}
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

// TestPackageSetInstaller_Install_PopulatesVersions verifies that Install
// runs the pre-install IsInstalled probes, then apt-get install, then
// version probes per package in order, and the per-package versions land
// in state.InstalledVersions.
func TestPackageSetInstaller_Install_PopulatesVersions(t *testing.T) {
	t.Parallel()
	runner := &mockCommandRunner{
		// Sequence:
		//   indices 0..1: pre-install IsInstalled probes for tree, jq
		//                 (empty output ⇒ not installed).
		//   index   2   : apt-get install (no output read).
		//   indices 3..4: dpkg-query version probes per package.
		captureOutputs: []string{
			"",
			"",
			"",
			"installed 1.8.0-5build1\n",
			"installed 1.6-2.1ubuntu3\n",
		},
	}
	res := &resource.SystemPackageSet{
		SystemPackageSetSpec: &resource.SystemPackageSetSpec{
			InstallerRef: "apt",
			Packages:     []string{"tree", "jq"},
		},
	}
	state, err := New(runner).PackageSetInstaller().Install(context.Background(), res, "cli-tools")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t,
		map[string]string{"tree": "1.8.0-5build1", "jq": "1.6-2.1ubuntu3"},
		state.InstalledVersions,
	)
	// Verify the full call sequence: pre-install probes, then install,
	// then version probes — all in spec order.
	require.Len(t, runner.captureCmds, 5)
	assert.Equal(t, `dpkg-query -W -f='${db:Status-Status}\n' -- tree`, runner.captureCmds[0])
	assert.Equal(t, `dpkg-query -W -f='${db:Status-Status}\n' -- jq`, runner.captureCmds[1])
	assert.Equal(t,
		"sudo -n env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C apt-get install -y -o DPkg::Lock::Timeout=60 -- tree jq",
		runner.captureCmds[2],
	)
	assert.Equal(t, `dpkg-query -W -f='${db:Status-Status} ${Version}\n' -- tree`, runner.captureCmds[3])
	assert.Equal(t, `dpkg-query -W -f='${db:Status-Status} ${Version}\n' -- jq`, runner.captureCmds[4])
}

// TestPackageSetInstaller_Install_Upgrade_DrainsDroppedPackages
// asserts the upgrade-time package drainage. When the executor invokes
// Install with executor.WithOldPackages on the ctx (as it does for any
// ActionUpgrade / ActionReinstall on a state implementing GetPackages),
// the installer MUST uninstall the diff (old - new) BEFORE running
// apt-get install for the new spec. Without this, a user shrinking the
// spec from [tree, jq] to [tree] would leave jq installed on the host
// with no state tracking — the executor's generic "upgrade = re-run
// Install" flow has no other place to do this cleanup.
func TestPackageSetInstaller_Install_Upgrade_DrainsDroppedPackages(t *testing.T) {
	t.Parallel()
	runner := &mockCommandRunner{
		// Sequence (new spec = [tree], old packages = [tree, jq] from
		// executor ctx):
		//   index 0: drainage apt-get remove jq.
		//   index 1: pre-install IsInstalled(tree) → "installed\n".
		//   index 2: apt-get install tree (no-op, already installed).
		//   index 3: dpkg-query version(tree).
		captureOutputs: []string{"", "installed\n", "", "installed 1.8.0-5build1\n"},
	}
	res := &resource.SystemPackageSet{
		SystemPackageSetSpec: &resource.SystemPackageSetSpec{
			InstallerRef: "apt",
			Packages:     []string{"tree"},
		},
	}
	ctx := executor.WithOldPackages(context.Background(), []string{"tree", "jq"})
	state, err := New(runner).PackageSetInstaller().Install(ctx, res, "cli-tools")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, []string{"tree"}, state.Packages, "state reflects the new spec only")
	require.Len(t, runner.captureCmds, 4)
	assert.Equal(t,
		"sudo -n env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C apt-get remove -y -o DPkg::Lock::Timeout=60 -- jq",
		runner.captureCmds[0],
		"drainage must remove jq (in old state, not in new spec) BEFORE the install",
	)
	assert.Equal(t, `dpkg-query -W -f='${db:Status-Status}\n' -- tree`, runner.captureCmds[1])
	assert.Equal(t,
		"sudo -n env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C apt-get install -y -o DPkg::Lock::Timeout=60 -- tree",
		runner.captureCmds[2],
	)
}

// TestPackageSetInstaller_Install_Upgrade_NoDrainWhenSpecGrows asserts
// that when the new spec is a superset of the old (no packages dropped),
// no drainage apt-get remove fires — only the install + version probes.
func TestPackageSetInstaller_Install_Upgrade_NoDrainWhenSpecGrows(t *testing.T) {
	t.Parallel()
	runner := &mockCommandRunner{
		captureOutputs: []string{
			"installed\n", // pre-install IsInstalled(tree) → already installed
			"",            // pre-install IsInstalled(jq)   → not installed
			"",            // apt-get install (no output read)
			"installed 1.8.0-5build1\n",
			"installed 1.6-2.1ubuntu3\n",
		},
	}
	res := &resource.SystemPackageSet{
		SystemPackageSetSpec: &resource.SystemPackageSetSpec{
			InstallerRef: "apt",
			Packages:     []string{"tree", "jq"},
		},
	}
	ctx := executor.WithOldPackages(context.Background(), []string{"tree"})
	state, err := New(runner).PackageSetInstaller().Install(ctx, res, "cli-tools")
	require.NoError(t, err)
	require.NotNil(t, state)
	require.Len(t, runner.captureCmds, 5,
		"superset upgrade ⇒ no drainage; sequence is 2 IsInstalled + 1 install + 2 version probes")
	// First command must be a dpkg-query (pre-install IsInstalled probe),
	// NOT an apt-get remove. This pins the "no drainage" claim.
	assert.Contains(t, runner.captureCmds[0], "dpkg-query",
		"superset upgrade must NOT run apt-get remove first")
}

// TestPackageSetInstaller_Install_Upgrade_DrainFailureAborts asserts
// that a drainage failure (apt-get remove returning an error) aborts the
// Install before any apt-get install runs. The host stays in its
// pre-Install state if apt-get remove fails on the dropped packages.
func TestPackageSetInstaller_Install_Upgrade_DrainFailureAborts(t *testing.T) {
	t.Parallel()
	drainErr := errors.New("apt-get remove: exit status 100")
	runner := &mockCommandRunner{
		// Drainage (index 0) fails. The pre-install probes and
		// apt-get install MUST NOT run.
		captureErrs: []error{drainErr},
	}
	res := &resource.SystemPackageSet{
		SystemPackageSetSpec: &resource.SystemPackageSetSpec{
			InstallerRef: "apt",
			Packages:     []string{"tree"},
		},
	}
	ctx := executor.WithOldPackages(context.Background(), []string{"tree", "jq"})
	state, err := New(runner).PackageSetInstaller().Install(ctx, res, "cli-tools")
	require.Error(t, err)
	assert.Nil(t, state)
	assert.Contains(t, err.Error(), `apt: package set "cli-tools": upgrade drain`)
	require.Len(t, runner.captureCmds, 1, "drainage failure must abort BEFORE the install step")
}

// TestPackageSetInstaller_Install_VersionProbeError verifies that a
// failure in any post-install dpkg-query probe aborts the Install and
// returns the wrapped error — a successful apt-get install for which we
// cannot read versions is a state-store consistency bug, so the helper
// surfaces it rather than degrading to an empty map.
//
// Beyond surfacing the error, Install must also roll back the apt-get
// install (best-effort) so the host returns to its pre-Install state —
// but ONLY for packages that this Install action newly placed. Packages
// already on the host before Install must NOT be uninstalled, even if
// they appear in spec.Packages (apt-get install no-ops on preexisting
// packages; the rollback diff is based on the pre-install IsInstalled
// snapshot). This mirrors PackageRepositoryInstaller.bestEffortRollback's
// snapshot-restore semantics for repository files.
func TestPackageSetInstaller_Install_VersionProbeError(t *testing.T) {
	t.Parallel()
	probeErr := errors.New("dpkg-query: connection lost")
	runner := &mockCommandRunner{
		// Sequence:
		//   index 0: pre-install IsInstalled(tree) → nil err, "" out → not installed.
		//   index 1: pre-install IsInstalled(jq)   → nil err, "" out → not installed.
		//   index 2: apt-get install               → nil err.
		//   index 3: dpkg-query version(tree)      → probeErr.
		//   index 4: best-effort rollback apt-get remove → nil err.
		captureErrs: []error{nil, nil, nil, probeErr, nil},
	}
	res := &resource.SystemPackageSet{
		SystemPackageSetSpec: &resource.SystemPackageSetSpec{
			InstallerRef: "apt",
			Packages:     []string{"tree", "jq"},
		},
	}
	state, err := New(runner).PackageSetInstaller().Install(context.Background(), res, "cli-tools")
	require.Error(t, err)
	assert.Nil(t, state, "no state when version probe fails — state-store consistency")
	assert.Contains(t, err.Error(), `apt: package set "cli-tools": probe version of "tree"`)
	// Probe for "jq" must NOT fire after "tree"'s probe failed, but the
	// best-effort rollback must remove BOTH packages (both were absent
	// before Install — the snapshot recorded them as not-installed, so
	// both are in the rollback set).
	require.Len(t, runner.captureCmds, 5)
	assert.Equal(t, `dpkg-query -W -f='${db:Status-Status}\n' -- tree`, runner.captureCmds[0])
	assert.Equal(t, `dpkg-query -W -f='${db:Status-Status}\n' -- jq`, runner.captureCmds[1])
	assert.Equal(t,
		"sudo -n env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C apt-get install -y -o DPkg::Lock::Timeout=60 -- tree jq",
		runner.captureCmds[2],
	)
	assert.Equal(t, `dpkg-query -W -f='${db:Status-Status} ${Version}\n' -- tree`, runner.captureCmds[3])
	assert.Equal(t,
		"sudo -n env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C apt-get remove -y -o DPkg::Lock::Timeout=60 -- tree jq",
		runner.captureCmds[4],
	)
}

// TestPackageSetInstaller_Install_VersionProbeError_PreinstalledNotRolledBack
// asserts the rollback differential: a package that was already installed
// before Install must NOT be removed during the post-probe-failure rollback.
// Without the pre-install snapshot, the rollback would uninstall a user-
// managed (or other SystemPackageSet's) package, violating the "do not
// touch what we did not place" contract.
func TestPackageSetInstaller_Install_VersionProbeError_PreinstalledNotRolledBack(t *testing.T) {
	t.Parallel()
	probeErr := errors.New("dpkg-query: connection lost")
	runner := &mockCommandRunner{
		// Sequence:
		//   index 0: pre-install IsInstalled(tree) → "installed\n" ⇒ already on host.
		//   index 1: pre-install IsInstalled(jq)   → "" ⇒ not installed.
		//   index 2: apt-get install (no-ops tree, installs jq).
		//   index 3: dpkg-query version(tree) → probeErr.
		//   index 4: best-effort rollback apt-get remove → nil. Must only
		//            target jq because tree predated this Install action.
		captureOutputs: []string{"installed\n", "", "", "", ""},
		captureErrs:    []error{nil, nil, nil, probeErr, nil},
	}
	res := &resource.SystemPackageSet{
		SystemPackageSetSpec: &resource.SystemPackageSetSpec{
			InstallerRef: "apt",
			Packages:     []string{"tree", "jq"},
		},
	}
	state, err := New(runner).PackageSetInstaller().Install(context.Background(), res, "cli-tools")
	require.Error(t, err)
	assert.Nil(t, state)
	assert.Contains(t, err.Error(), `apt: package set "cli-tools": probe version of "tree"`)
	require.Len(t, runner.captureCmds, 5)
	// Critical assertion: the rollback removes ONLY jq, not tree.
	assert.Equal(t,
		"sudo -n env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C apt-get remove -y -o DPkg::Lock::Timeout=60 -- jq",
		runner.captureCmds[4],
		"rollback must not uninstall packages that predated this Install action",
	)
}

// TestPackageSetInstaller_Install_VersionProbeError_AllPreinstalledNoRollback
// asserts that when every package was already on the host, a probe
// failure skips rollback entirely (no apt-get remove call). The Install
// action did not change the host's installed-package set, so there is
// nothing to undo.
func TestPackageSetInstaller_Install_VersionProbeError_AllPreinstalledNoRollback(t *testing.T) {
	t.Parallel()
	probeErr := errors.New("dpkg-query: connection lost")
	runner := &mockCommandRunner{
		// Both pre-install probes report "installed". Install no-ops.
		// The version probe fails on tree → no rollback (nothing newly placed).
		captureOutputs: []string{"installed\n", "installed\n", "", "", ""},
		captureErrs:    []error{nil, nil, nil, probeErr},
	}
	res := &resource.SystemPackageSet{
		SystemPackageSetSpec: &resource.SystemPackageSetSpec{
			InstallerRef: "apt",
			Packages:     []string{"tree", "jq"},
		},
	}
	state, err := New(runner).PackageSetInstaller().Install(context.Background(), res, "cli-tools")
	require.Error(t, err)
	assert.Nil(t, state)
	// No rollback command: sequence stops at the failing version probe.
	require.Len(t, runner.captureCmds, 4,
		"all packages preinstalled ⇒ no rollback fires; sequence is 2 IsInstalled + 1 install + 1 failing probe")
}

// TestPackageSetInstaller_Install_VersionProbeError_RollbackFailure
// verifies that a rollback failure does NOT mask the probe error in the
// returned wrap — the trigger cause (probe failure) stays at the head of
// the chain; the rollback failure goes to slog at Warn. The host may end
// up with orphaned packages in this rare path, but the apply still fails
// loudly with the right root cause.
func TestPackageSetInstaller_Install_VersionProbeError_RollbackFailure(t *testing.T) {
	t.Parallel()
	probeErr := errors.New("dpkg-query: connection lost")
	rollbackErr := errors.New("apt-get remove: exit status 100")
	runner := &mockCommandRunner{
		// Sequence:
		//   index 0: pre-install IsInstalled(tree) → not installed.
		//   index 1: apt-get install → nil.
		//   index 2: dpkg-query version(tree) → probeErr.
		//   index 3: rollback apt-get remove → rollbackErr.
		captureErrs: []error{nil, nil, probeErr, rollbackErr},
	}
	res := &resource.SystemPackageSet{
		SystemPackageSetSpec: &resource.SystemPackageSetSpec{
			InstallerRef: "apt",
			Packages:     []string{"tree"},
		},
	}
	state, err := New(runner).PackageSetInstaller().Install(context.Background(), res, "cli-tools")
	require.Error(t, err)
	assert.Nil(t, state)
	// The trigger error must be the head of the chain; the rollback
	// failure goes to slog (asserted by other tests via capture pattern).
	assert.Contains(t, err.Error(), "probe version of")
	assert.Contains(t, err.Error(), "dpkg-query: connection lost")
	// The rollback attempt still fired even though it failed.
	require.Len(t, runner.captureCmds, 4)
	assert.Equal(t,
		"sudo -n env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C apt-get remove -y -o DPkg::Lock::Timeout=60 -- tree",
		runner.captureCmds[3],
	)
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
