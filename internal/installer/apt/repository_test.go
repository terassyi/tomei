package apt

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/installer/download"
	"github.com/terassyi/tomei/internal/resource"
)

// mockDownloader is a test-only download.Downloader. Download writes
// downloadBody to destPath (or returns downloadErr); Verify is a
// no-op or returns verifyErr; calls counters expose ordering.
type mockDownloader struct {
	downloadBody []byte
	downloadErr  error
	verifyErr    error
	downloadURLs []string
	verifyPaths  []string
}

var _ download.Downloader = (*mockDownloader)(nil)

func (m *mockDownloader) Download(_ context.Context, url, destPath string) (string, error) {
	m.downloadURLs = append(m.downloadURLs, url)
	if m.downloadErr != nil {
		return "", m.downloadErr
	}
	if err := os.WriteFile(destPath, m.downloadBody, 0o600); err != nil {
		return "", err
	}
	return destPath, nil
}

func (m *mockDownloader) DownloadWithProgress(ctx context.Context, url, destPath string, _ download.ProgressCallback) (string, error) {
	return m.Download(ctx, url, destPath)
}

func (m *mockDownloader) Verify(_ context.Context, filePath string, _ *resource.Checksum) error {
	m.verifyPaths = append(m.verifyPaths, filePath)
	return m.verifyErr
}

// dockerSpec returns a complete SystemPackageRepository spec used by
// repository tests. The values mirror the canonical Docker APT
// repository so any divergence in builder logic is obvious.
func dockerSpec(name string) *resource.SystemPackageRepository {
	return &resource.SystemPackageRepository{
		BaseResource: resource.BaseResource{
			APIVersion:   "tomei.terassyi.net/v1beta1",
			ResourceKind: resource.KindSystemPackageRepository,
			Metadata:     resource.Metadata{Name: name},
		},
		SystemPackageRepositorySpec: &resource.SystemPackageRepositorySpec{
			InstallerRef: "apt",
			Source: resource.SourceConfig{
				URL:        "https://download.docker.com/linux/ubuntu",
				KeyURL:     "https://download.docker.com/linux/ubuntu/gpg",
				KeyHash:    "sha256:1500c1f56fa9e26b9b8f42452a553675796ade0807cdce11975eb98170b3a570",
				Suite:      "jammy",
				Components: []string{"stable"},
				Options:    map[string]string{"arch": "amd64"},
			},
		},
	}
}

// --- buildSourcesListLine ---

func TestBuildSourcesListLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		repo    string
		src     resource.SourceConfig
		want    string
		wantErr string
	}{
		{
			name: "single component, signed-by auto, arch set",
			repo: "docker",
			src: resource.SourceConfig{
				URL:        "https://download.docker.com/linux/ubuntu",
				Suite:      "jammy",
				Components: []string{"stable"},
				Options:    map[string]string{"arch": "amd64"},
			},
			want: "deb [arch=amd64 signed-by=/usr/share/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu jammy stable\n",
		},
		{
			name: "multiple components",
			repo: "vendor",
			src: resource.SourceConfig{
				URL:        "https://example.com/ubuntu",
				Suite:      "noble",
				Components: []string{"main", "contrib", "non-free"},
			},
			want: "deb [signed-by=/usr/share/keyrings/vendor.gpg] https://example.com/ubuntu noble main contrib non-free\n",
		},
		{
			name: "empty component string rejected",
			repo: "x",
			src: resource.SourceConfig{
				URL:        "https://x",
				Suite:      "s",
				Components: []string{""},
			},
			wantErr: "components[0]",
		},
		{
			name: "component with whitespace rejected",
			repo: "x",
			src: resource.SourceConfig{
				URL:        "https://x",
				Suite:      "s",
				Components: []string{" "},
			},
			wantErr: "components[0]",
		},
		{
			name: "signed-by override accepted",
			repo: "vendor",
			src: resource.SourceConfig{
				URL:        "https://example.com/repo",
				Suite:      "stable",
				Components: []string{"main"},
				Options: map[string]string{
					"signed-by": "/etc/apt/keyrings/legacy.gpg",
				},
			},
			want: "deb [signed-by=/etc/apt/keyrings/legacy.gpg] https://example.com/repo stable main\n",
		},
		{
			name: "deterministic option order",
			repo: "ordered",
			src: resource.SourceConfig{
				URL:        "https://example.com/repo",
				Suite:      "main",
				Components: []string{"all"},
				Options: map[string]string{
					"arch":              "amd64",
					"by-hash":           "yes",
					"check-valid-until": "no",
				},
			},
			want: "deb [arch=amd64 by-hash=yes check-valid-until=no signed-by=/usr/share/keyrings/ordered.gpg] https://example.com/repo main all\n",
		},
		{
			name:    "empty name rejected",
			repo:    "",
			src:     resource.SourceConfig{URL: "https://x", Suite: "s", Components: []string{"c"}},
			wantErr: "empty repository name",
		},
		{
			name:    "name with slash rejected",
			repo:    "evil/name",
			src:     resource.SourceConfig{URL: "https://x", Suite: "s", Components: []string{"c"}},
			wantErr: "contains disallowed characters",
		},
		{
			name:    "name with traversal rejected",
			repo:    "../etc/passwd",
			src:     resource.SourceConfig{URL: "https://x", Suite: "s", Components: []string{"c"}},
			wantErr: "contains disallowed characters",
		},
		{
			name:    "empty URL rejected",
			repo:    "x",
			src:     resource.SourceConfig{URL: "", Suite: "s", Components: []string{"c"}},
			wantErr: "url",
		},
		{
			name:    "URL with newline rejected",
			repo:    "x",
			src:     resource.SourceConfig{URL: "https://x\nhttps://attacker", Suite: "s", Components: []string{"c"}},
			wantErr: "url",
		},
		{
			name:    "empty suite rejected",
			repo:    "x",
			src:     resource.SourceConfig{URL: "https://x", Suite: "", Components: []string{"c"}},
			wantErr: "suite",
		},
		{
			name:    "empty components rejected",
			repo:    "x",
			src:     resource.SourceConfig{URL: "https://x", Suite: "s", Components: nil},
			wantErr: "components must have at least one entry",
		},
		{
			name:    "unknown option rejected",
			repo:    "x",
			src:     resource.SourceConfig{URL: "https://x", Suite: "s", Components: []string{"c"}, Options: map[string]string{"trusted": "yes"}},
			wantErr: `option "trusted" is not allowed`,
		},
		{
			name:    "option with newline rejected",
			repo:    "x",
			src:     resource.SourceConfig{URL: "https://x", Suite: "s", Components: []string{"c"}, Options: map[string]string{"arch": "amd64\nhostile"}},
			wantErr: "line-ending or NUL byte",
		},
		{
			name:    "option with bracket rejected",
			repo:    "x",
			src:     resource.SourceConfig{URL: "https://x", Suite: "s", Components: []string{"c"}, Options: map[string]string{"arch": "amd64]"}},
			wantErr: "bracket or equals character",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := buildSourcesListLine(tt.repo, tt.src)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Empty(t, got)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// --- Install: success path ---

func TestPackageRepositoryInstaller_Install_Success(t *testing.T) {
	t.Parallel()
	runner := &mockCommandRunner{}
	dl := &mockDownloader{downloadBody: []byte("armored key body")}

	state, err := New(runner).PackageRepositoryInstaller(dl).Install(context.Background(), dockerSpec("docker"), "docker")
	require.NoError(t, err)
	require.NotNil(t, state)

	// Downloader contract: one Download + one Verify, in order.
	require.Len(t, dl.downloadURLs, 1)
	assert.Equal(t, "https://download.docker.com/linux/ubuntu/gpg", dl.downloadURLs[0])
	require.Len(t, dl.verifyPaths, 1)

	// Shell call sequence: 4 calls (dearmor, install keyring, install
	// sources, apt-get update). Each call has exactly one cmd string.
	require.Len(t, runner.captureCallCmds, 4)
	for i, cmds := range runner.captureCallCmds {
		require.Len(t, cmds, 1, "call %d", i)
	}

	// Sub-string anchors on each cmd (full strings vary by tmpDir path).
	assert.Contains(t, runner.captureCallCmds[0][0], "gpg --dearmor")
	assert.Contains(t, runner.captureCallCmds[1][0], "sudo -n install -D -m 0644 -o root -g root --")
	assert.Contains(t, runner.captureCallCmds[1][0], keyringPath("docker"))
	assert.Contains(t, runner.captureCallCmds[2][0], "sudo -n install -D -m 0644 -o root -g root --")
	assert.Contains(t, runner.captureCallCmds[2][0], sourcesListPath("docker"))
	assert.Equal(t,
		"sudo -n env DEBIAN_FRONTEND=noninteractive apt-get update 2>&1",
		runner.captureCallCmds[3][0])

	// State contract: keyring first, then sources.list.
	assert.Equal(t, "apt", state.InstallerRef)
	assert.Equal(t, []string{
		keyringPath("docker"),
		sourcesListPath("docker"),
	}, state.InstalledFiles)
}

// --- Install: error paths ---

func TestPackageRepositoryInstaller_Install_NilSpec(t *testing.T) {
	t.Parallel()
	runner := &mockCommandRunner{}
	dl := &mockDownloader{}
	_, err := New(runner).PackageRepositoryInstaller(dl).Install(context.Background(), nil, "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil spec")
}

func TestPackageRepositoryInstaller_Install_NameRejected(t *testing.T) {
	t.Parallel()
	runner := &mockCommandRunner{}
	dl := &mockDownloader{}
	res := dockerSpec("evil/name")
	_, err := New(runner).PackageRepositoryInstaller(dl).Install(context.Background(), res, "evil/name")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "contains disallowed characters")
	// No host mutation, no download attempted.
	assert.Empty(t, runner.captureCallCmds)
	assert.Empty(t, dl.downloadURLs)
}

func TestPackageRepositoryInstaller_Install_DownloadFailure(t *testing.T) {
	t.Parallel()
	runner := &mockCommandRunner{}
	dl := &mockDownloader{downloadErr: errors.New("http 503")}
	_, err := New(runner).PackageRepositoryInstaller(dl).Install(context.Background(), dockerSpec("docker"), "docker")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `apt: repository "docker": download key`)
	// Nothing on the host yet.
	assert.Empty(t, runner.captureCallCmds)
}

func TestPackageRepositoryInstaller_Install_VerifyFailure(t *testing.T) {
	t.Parallel()
	runner := &mockCommandRunner{}
	dl := &mockDownloader{downloadBody: []byte("body"), verifyErr: errors.New("hash mismatch")}
	_, err := New(runner).PackageRepositoryInstaller(dl).Install(context.Background(), dockerSpec("docker"), "docker")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `apt: repository "docker": verify key`)
	assert.Empty(t, runner.captureCallCmds)
}

func TestPackageRepositoryInstaller_Install_DearmorFailure(t *testing.T) {
	t.Parallel()
	runner := &mockCommandRunner{captureErrs: []error{errors.New("gpg failed")}}
	dl := &mockDownloader{downloadBody: []byte("body")}
	_, err := New(runner).PackageRepositoryInstaller(dl).Install(context.Background(), dockerSpec("docker"), "docker")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `apt: repository "docker": dearmor key`)
	// Only dearmor was attempted (no keyring install yet).
	require.Len(t, runner.captureCallCmds, 1)
}

func TestPackageRepositoryInstaller_Install_KeyringInstallFailure(t *testing.T) {
	t.Parallel()
	// call 0 (dearmor) succeeds; call 1 (install keyring) fails.
	runner := &mockCommandRunner{captureErrs: []error{nil, errors.New("sudo denied")}}
	dl := &mockDownloader{downloadBody: []byte("body")}
	_, err := New(runner).PackageRepositoryInstaller(dl).Install(context.Background(), dockerSpec("docker"), "docker")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `apt: repository "docker": install keyring`)
	// Stopped after the failure: dearmor + install keyring only.
	require.Len(t, runner.captureCallCmds, 2)
}

func TestPackageRepositoryInstaller_Install_SourcesInstallFailure_RollsBackKeyring(t *testing.T) {
	t.Parallel()
	// call 0 dearmor ok, call 1 install keyring ok, call 2 install sources fails,
	// call 3 must be the rollback `sudo rm -f --` of the keyring.
	runner := &mockCommandRunner{
		captureErrs: []error{nil, nil, errors.New("sudo denied"), nil},
	}
	dl := &mockDownloader{downloadBody: []byte("body")}
	_, err := New(runner).PackageRepositoryInstaller(dl).Install(context.Background(), dockerSpec("docker"), "docker")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `apt: repository "docker": install sources`)
	require.Len(t, runner.captureCallCmds, 4)
	assert.Contains(t, runner.captureCallCmds[3][0], "sudo -n rm -f --")
	assert.Contains(t, runner.captureCallCmds[3][0], keyringPath("docker"))
}

func TestPackageRepositoryInstaller_Install_UpdateFailure_RollsBackBoth(t *testing.T) {
	t.Parallel()
	// dearmor ok, install keyring ok, install sources ok, update fails
	// hard (non-zero exit) → rollback both placed files. The follow-up
	// update is intentionally skipped on hard failure because the cache
	// is already in an indeterminate state and re-running won't help.
	runner := &mockCommandRunner{
		captureErrs: []error{nil, nil, nil, errors.New("update broke")},
	}
	dl := &mockDownloader{downloadBody: []byte("body")}
	_, err := New(runner).PackageRepositoryInstaller(dl).Install(context.Background(), dockerSpec("docker"), "docker")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `apt: repository "docker": update`)
	// 4 (success path) + 2 rollback rm = 6 calls.
	require.Len(t, runner.captureCallCmds, 6)
	assert.Contains(t, runner.captureCallCmds[4][0], "sudo -n rm -f --")
	assert.Contains(t, runner.captureCallCmds[4][0], sourcesListPath("docker"))
	assert.Contains(t, runner.captureCallCmds[5][0], "sudo -n rm -f --")
	assert.Contains(t, runner.captureCallCmds[5][0], keyringPath("docker"))
}

func TestPackageRepositoryInstaller_Install_PartialFetchFailure_RollsBackBoth(t *testing.T) {
	t.Parallel()
	// All four success-path calls succeed; the apt-get update output
	// contains a `W: Failed to fetch` line targeting the configured URL,
	// triggering rollback even though exit code was zero.
	failedOutput := `Hit:1 https://archive.ubuntu.com/ubuntu jammy InRelease
W: Failed to fetch https://download.docker.com/linux/ubuntu/dists/jammy/InRelease  404 Not Found
Reading package lists... Done
`
	runner := &mockCommandRunner{
		captureOutputs: []string{"", "", "", failedOutput},
	}
	dl := &mockDownloader{downloadBody: []byte("body")}
	_, err := New(runner).PackageRepositoryInstaller(dl).Install(context.Background(), dockerSpec("docker"), "docker")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `apt: repository "docker": failed to fetch`)
	assert.Contains(t, err.Error(), "https://download.docker.com/linux/ubuntu/dists/jammy/InRelease")
	require.Len(t, runner.captureCallCmds, 7)
	assert.Contains(t, runner.captureCallCmds[4][0], "sudo -n rm -f --")
	assert.Contains(t, runner.captureCallCmds[4][0], sourcesListPath("docker"))
	assert.Contains(t, runner.captureCallCmds[5][0], "sudo -n rm -f --")
	assert.Contains(t, runner.captureCallCmds[5][0], keyringPath("docker"))
}

func TestPackageRepositoryInstaller_Install_PartialFetchFailure_UnrelatedURLIgnored(t *testing.T) {
	t.Parallel()
	// Failed-to-fetch warnings about unrelated mirrors must NOT trigger
	// rollback; only failures rooted in our repo's URL count.
	output := `W: Failed to fetch https://other.example.com/dists/foo/InRelease  404
Reading package lists... Done
`
	runner := &mockCommandRunner{
		captureOutputs: []string{"", "", "", output},
	}
	dl := &mockDownloader{downloadBody: []byte("body")}
	state, err := New(runner).PackageRepositoryInstaller(dl).Install(context.Background(), dockerSpec("docker"), "docker")
	require.NoError(t, err)
	require.NotNil(t, state)
	// Exactly the 4 success-path commands, no rollback.
	require.Len(t, runner.captureCallCmds, 4)
}

// TestPackageRepositoryInstaller_Install_UpdateFailure_RollbackRmAlsoFails_DoesNotMask
// verifies that when the apt-get update step fails AND the rollback rm
// of the placed files also fails, the returned error still attributes
// the original (update) failure rather than the rollback. This pins
// the "best-effort rollback" contract: callers must learn the cause,
// not the cleanup symptom.
func TestPackageRepositoryInstaller_Install_UpdateFailure_RollbackRmAlsoFails_DoesNotMask(t *testing.T) {
	t.Parallel()
	runner := &mockCommandRunner{
		captureErrs: []error{
			nil,                        // dearmor
			nil,                        // install keyring
			nil,                        // install sources
			errors.New("update broke"), // update fails
			errors.New("sudo denied"),  // rollback rm sources fails
			errors.New("sudo denied"),  // rollback rm keyring fails
		},
	}
	dl := &mockDownloader{downloadBody: []byte("body")}
	_, err := New(runner).PackageRepositoryInstaller(dl).Install(context.Background(), dockerSpec("docker"), "docker")
	require.Error(t, err)
	// The original update failure remains the surfaced cause.
	assert.Contains(t, err.Error(), `apt: repository "docker": update`)
	assert.Contains(t, err.Error(), "update broke")
	assert.NotContains(t, err.Error(), "sudo denied")
}

// --- Remove ---

func TestPackageRepositoryInstaller_Remove_Success(t *testing.T) {
	t.Parallel()
	runner := &mockCommandRunner{}
	state := &resource.SystemPackageRepositoryState{
		InstalledFiles: []string{
			keyringPath("docker"),
			sourcesListPath("docker"),
		},
	}
	err := New(runner).PackageRepositoryInstaller(&mockDownloader{}).Remove(context.Background(), state, "docker")
	require.NoError(t, err)
	// Reverse order: sources.list first, then keyring, then update.
	require.Len(t, runner.captureCallCmds, 3)
	assert.Contains(t, runner.captureCallCmds[0][0], "sudo -n rm -f --")
	assert.Contains(t, runner.captureCallCmds[0][0], sourcesListPath("docker"))
	assert.Contains(t, runner.captureCallCmds[1][0], "sudo -n rm -f --")
	assert.Contains(t, runner.captureCallCmds[1][0], keyringPath("docker"))
	assert.Equal(t,
		"sudo -n env DEBIAN_FRONTEND=noninteractive apt-get update",
		runner.captureCallCmds[2][0])
}

func TestPackageRepositoryInstaller_Remove_NilState(t *testing.T) {
	t.Parallel()
	runner := &mockCommandRunner{}
	err := New(runner).PackageRepositoryInstaller(&mockDownloader{}).Remove(context.Background(), nil, "docker")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil state")
	assert.Empty(t, runner.captureCallCmds)
}

func TestPackageRepositoryInstaller_Remove_PathNonCanonical(t *testing.T) {
	t.Parallel()
	runner := &mockCommandRunner{}
	state := &resource.SystemPackageRepositoryState{
		// path looks like it's under the allowlist but contains `..`
		// segments — must be refused before any rm fires.
		InstalledFiles: []string{
			"/usr/share/keyrings/../../etc/passwd",
		},
	}
	err := New(runner).PackageRepositoryInstaller(&mockDownloader{}).Remove(context.Background(), state, "docker")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-canonical")
	assert.Empty(t, runner.captureCallCmds)
}

func TestPackageRepositoryInstaller_Remove_PathOutsideAllowlist(t *testing.T) {
	t.Parallel()
	runner := &mockCommandRunner{}
	state := &resource.SystemPackageRepositoryState{
		InstalledFiles: []string{
			keyringPath("docker"),
			"/etc/passwd",
		},
	}
	err := New(runner).PackageRepositoryInstaller(&mockDownloader{}).Remove(context.Background(), state, "docker")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside")
	// The first iteration (sources.list slot, here /etc/passwd) was
	// rejected before any rm fired.
	assert.Empty(t, runner.captureCallCmds)
}

func TestPackageRepositoryInstaller_Remove_RmFailure(t *testing.T) {
	t.Parallel()
	runner := &mockCommandRunner{captureErrs: []error{errors.New("sudo denied")}}
	state := &resource.SystemPackageRepositoryState{
		InstalledFiles: []string{
			keyringPath("docker"),
			sourcesListPath("docker"),
		},
	}
	err := New(runner).PackageRepositoryInstaller(&mockDownloader{}).Remove(context.Background(), state, "docker")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remove")
}

// --- failedToFetchURLs ---

func TestFailedToFetchURLs(t *testing.T) {
	t.Parallel()
	base := "https://download.docker.com/linux/ubuntu"
	output := `Hit:1 https://archive.ubuntu.com/ubuntu jammy InRelease
W: Failed to fetch https://download.docker.com/linux/ubuntu/dists/jammy/InRelease  404 Not Found
W: Failed to fetch https://other.example.com/foo  500
Reading package lists... Done
`
	hits := failedToFetchURLs(output, base)
	require.Len(t, hits, 1)
	assert.Equal(t, "https://download.docker.com/linux/ubuntu/dists/jammy/InRelease", hits[0])
}

func TestFailedToFetchURLs_EmptyBase(t *testing.T) {
	t.Parallel()
	hits := failedToFetchURLs("W: Failed to fetch https://x/y\n", "")
	assert.Empty(t, hits)
}

// TestFailedToFetchURLs_MultipleHits pins the contract that every URL
// rooted under the base is returned, not just the first match. apt-get
// can fail to fetch InRelease, Packages, and Translation-* files for
// the same repo on a single run.
func TestFailedToFetchURLs_MultipleHits(t *testing.T) {
	t.Parallel()
	base := "https://download.docker.com/linux/ubuntu"
	output := `W: Failed to fetch https://download.docker.com/linux/ubuntu/dists/jammy/InRelease  404
W: Failed to fetch https://download.docker.com/linux/ubuntu/dists/jammy/main/binary-amd64/Packages  503
`
	hits := failedToFetchURLs(output, base)
	require.Len(t, hits, 2)
	assert.Equal(t, "https://download.docker.com/linux/ubuntu/dists/jammy/InRelease", hits[0])
	assert.Equal(t, "https://download.docker.com/linux/ubuntu/dists/jammy/main/binary-amd64/Packages", hits[1])
}

// TestFailedToFetchURLs_TrailingSlashNormalised verifies that a base
// URL ending in `/` is treated equivalently to one without — apt's
// actual fetch URL always has additional path segments after the base,
// so a trailing slash in the manifest's URL should not cause a false
// negative.
func TestFailedToFetchURLs_TrailingSlashNormalised(t *testing.T) {
	t.Parallel()
	output := "W: Failed to fetch https://example.com/repo/dists/stable/InRelease  404\n"
	hits := failedToFetchURLs(output, "https://example.com/repo/")
	require.Len(t, hits, 1)
}

// TestFailedToFetchURLs_QueryStringAndIPv6 verifies the prefix match
// works regardless of URL features (query strings, IPv6 hosts). The
// regex `\S+` already excludes whitespace; the strings.HasPrefix
// check is the sole semantic guard.
func TestFailedToFetchURLs_QueryStringAndIPv6(t *testing.T) {
	t.Parallel()
	output := "W: Failed to fetch https://[::1]/repo/dists/main?arch=amd64  500\n"
	hits := failedToFetchURLs(output, "https://[::1]/repo")
	require.Len(t, hits, 1)
	assert.Equal(t, "https://[::1]/repo/dists/main?arch=amd64", hits[0])
}
