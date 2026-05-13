package apt

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
			InstallerRef: resource.InstallerRefApt,
			Apt: &resource.AptSource{
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
		src     *resource.AptSource
		want    string
		wantErr string
	}{
		{
			name: "single component, signed-by auto, arch set",
			repo: "docker",
			src: &resource.AptSource{
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
			src: &resource.AptSource{
				URL:        "https://example.com/ubuntu",
				Suite:      "noble",
				Components: []string{"main", "contrib", "non-free"},
			},
			want: "deb [signed-by=/usr/share/keyrings/vendor.gpg] https://example.com/ubuntu noble main contrib non-free\n",
		},
		{
			name: "empty component string rejected",
			repo: "x",
			src: &resource.AptSource{
				URL:        "https://x",
				Suite:      "s",
				Components: []string{""},
			},
			wantErr: "components[0]",
		},
		{
			name: "component with whitespace rejected",
			repo: "x",
			src: &resource.AptSource{
				URL:        "https://x",
				Suite:      "s",
				Components: []string{" "},
			},
			wantErr: "components[0]",
		},
		{
			// Defense-in-depth: even if a caller bypasses
			// AptSource.Validate (which rejects "signed-by" in Options)
			// and reaches buildSourcesListLine directly, the helper
			// unconditionally emits the canonical keyring path —
			// matching the install destination at sudo install time.
			// This prevents an install/emit divergence where the
			// sources.list could reference a path tomei did not write.
			name: "signed-by in options is overridden by auto-derive",
			repo: "vendor",
			src: &resource.AptSource{
				URL:        "https://example.com/repo",
				Suite:      "stable",
				Components: []string{"main"},
				Options: map[string]string{
					"signed-by": "/etc/apt/keyrings/legacy.gpg",
				},
			},
			want: "deb [signed-by=/usr/share/keyrings/vendor.gpg] https://example.com/repo stable main\n",
		},
		{
			name: "deterministic option order",
			repo: "ordered",
			src: &resource.AptSource{
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
			src:     &resource.AptSource{URL: "https://x", Suite: "s", Components: []string{"c"}},
			wantErr: "empty repository name",
		},
		{
			name:    "name with slash rejected",
			repo:    "evil/name",
			src:     &resource.AptSource{URL: "https://x", Suite: "s", Components: []string{"c"}},
			wantErr: "contains disallowed characters",
		},
		{
			name:    "name with traversal rejected",
			repo:    "../etc/passwd",
			src:     &resource.AptSource{URL: "https://x", Suite: "s", Components: []string{"c"}},
			wantErr: "contains disallowed characters",
		},
		{
			// `.` and `..` survive both the character allowlist and
			// filepath.Clean, but join to surprising on-disk targets
			// (/usr/share/keyrings/.gpg, /usr/share/keyrings/...gpg).
			// Rejected explicitly as defense-in-depth for non-CUE callers.
			name:    "name dot rejected",
			repo:    ".",
			src:     &resource.AptSource{URL: "https://x", Suite: "s", Components: []string{"c"}},
			wantErr: "reserved or dot-prefixed",
		},
		{
			name:    "name dotdot rejected",
			repo:    "..",
			src:     &resource.AptSource{URL: "https://x", Suite: "s", Components: []string{"c"}},
			wantErr: "reserved or dot-prefixed",
		},
		{
			name:    "name dot-prefix rejected",
			repo:    ".hidden",
			src:     &resource.AptSource{URL: "https://x", Suite: "s", Components: []string{"c"}},
			wantErr: "reserved or dot-prefixed",
		},
		{
			name:    "empty URL rejected",
			repo:    "x",
			src:     &resource.AptSource{URL: "", Suite: "s", Components: []string{"c"}},
			wantErr: "url",
		},
		{
			name:    "URL with newline rejected",
			repo:    "x",
			src:     &resource.AptSource{URL: "https://x\nhttps://attacker", Suite: "s", Components: []string{"c"}},
			wantErr: "url",
		},
		{
			name:    "empty suite rejected",
			repo:    "x",
			src:     &resource.AptSource{URL: "https://x", Suite: "", Components: []string{"c"}},
			wantErr: "suite",
		},
		{
			name:    "empty components rejected",
			repo:    "x",
			src:     &resource.AptSource{URL: "https://x", Suite: "s", Components: nil},
			wantErr: "components must have at least one entry",
		},
		// Note: unknown / disallowed option *keys* (e.g. "trusted",
		// "allow-insecure", "signed-by") are rejected by
		// AptSource.Validate (covered in
		// internal/resource/system_package_test.go), not by this helper.
		// buildSourcesListLine only validates option *values* — the
		// line-injection and shell-encoding concerns specific to
		// rendering into the sources.list file.
		{
			name:    "option with newline rejected",
			repo:    "x",
			src:     &resource.AptSource{URL: "https://x", Suite: "s", Components: []string{"c"}, Options: map[string]string{"arch": "amd64\nhostile"}},
			wantErr: "line-ending or NUL byte",
		},
		{
			name:    "option with bracket rejected",
			repo:    "x",
			src:     &resource.AptSource{URL: "https://x", Suite: "s", Components: []string{"c"}, Options: map[string]string{"arch": "amd64]"}},
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
	// Dearmor invocation uses --no-options + ephemeral --homedir to
	// neutralize any user ~/.gnupg/gpg.conf side-effects, and explicit
	// --output to avoid relying on shell redirection.
	assert.Contains(t, runner.captureCallCmds[0][0], "gpg ")
	assert.Contains(t, runner.captureCallCmds[0][0], "--no-default-keyring")
	assert.Contains(t, runner.captureCallCmds[0][0], "--no-options")
	assert.Contains(t, runner.captureCallCmds[0][0], "--homedir")
	assert.Contains(t, runner.captureCallCmds[0][0], "--dearmor")
	assert.Contains(t, runner.captureCallCmds[1][0], "sudo -n install -D -m 0644 -o root -g root --")
	assert.Contains(t, runner.captureCallCmds[1][0], keyringPath("docker"))
	assert.Contains(t, runner.captureCallCmds[2][0], "sudo -n install -D -m 0644 -o root -g root --")
	assert.Contains(t, runner.captureCallCmds[2][0], sourcesListPath("docker"))
	assert.Equal(t,
		"sudo -n env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C apt-get update",
		runner.captureCallCmds[3][0])

	// State contract: keyring first, then sources.list.
	assert.Equal(t, resource.InstallerRefApt, state.InstallerRef)
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

func TestPackageRepositoryInstaller_Install_KeyringInstallFailure_RollsBackKeyring(t *testing.T) {
	t.Parallel()
	// call 0 (dearmor) succeeds; call 1 (install keyring) fails; call 2
	// must be the rollback `sudo rm -f --` of the keyring — `install`
	// can leave a partially-created destination on error, so the rm is
	// load-bearing for the "no host state regression on failure"
	// contract.
	runner := &mockCommandRunner{captureErrs: []error{nil, errors.New("sudo denied"), nil}}
	dl := &mockDownloader{downloadBody: []byte("body")}
	_, err := New(runner).PackageRepositoryInstaller(dl).Install(context.Background(), dockerSpec("docker"), "docker")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `apt: repository "docker": install keyring`)
	require.Len(t, runner.captureCallCmds, 3)
	assert.Contains(t, runner.captureCallCmds[2][0], "sudo -n rm -f --")
	assert.Contains(t, runner.captureCallCmds[2][0], keyringPath("docker"))
}

func TestPackageRepositoryInstaller_Install_SourcesInstallFailure_RollsBackBoth(t *testing.T) {
	t.Parallel()
	// call 0 dearmor ok, call 1 install keyring ok, call 2 install sources
	// fails. Rollback must remove BOTH sourcesDst (the install command may
	// have partially placed it before failing) AND keyringDst, in that
	// order (sources first so a brief window doesn't leave a sources.list
	// pointing at a missing keyring). Calls 3 and 4 are the rm operations.
	runner := &mockCommandRunner{
		captureErrs: []error{nil, nil, errors.New("sudo denied"), nil, nil},
	}
	dl := &mockDownloader{downloadBody: []byte("body")}
	_, err := New(runner).PackageRepositoryInstaller(dl).Install(context.Background(), dockerSpec("docker"), "docker")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `apt: repository "docker": install sources`)
	require.Len(t, runner.captureCallCmds, 5)
	assert.Contains(t, runner.captureCallCmds[3][0], "sudo -n rm -f --")
	assert.Contains(t, runner.captureCallCmds[3][0], sourcesListPath("docker"))
	assert.Contains(t, runner.captureCallCmds[4][0], "sudo -n rm -f --")
	assert.Contains(t, runner.captureCallCmds[4][0], keyringPath("docker"))
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

// TestFailedToFetchURLs_PrefixBoundary pins the path-boundary rule:
// "https://example.com/repo" must NOT match a fetch failure URL of
// "https://example.com/repo-staging/...", even though one is a byte
// prefix of the other. Real-world collision: Google Cloud publishes both
// `https://packages.cloud.google.com/apt` and `.../apt-cli`; a failure
// in one repo must not trigger a rollback of the other.
func TestFailedToFetchURLs_PrefixBoundary(t *testing.T) {
	t.Parallel()
	output := `W: Failed to fetch https://example.com/repo-staging/dists/jammy/InRelease  404`
	hits := failedToFetchURLs(output, "https://example.com/repo")
	assert.Empty(t, hits, "must not attribute repo-staging fetch failure to repo")

	// Exact-match still hits.
	exact := `W: Failed to fetch https://example.com/repo`
	hits = failedToFetchURLs(exact, "https://example.com/repo")
	assert.Len(t, hits, 1, "exact-equal URL must hit")

	// Trailing-slash variants on either side are normalised.
	withSlash := `W: Failed to fetch https://example.com/repo/dists/jammy/InRelease  404`
	hits = failedToFetchURLs(withSlash, "https://example.com/repo/")
	assert.Len(t, hits, 1, "trailing-slash base must hit slash-anchored fetch URL")
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
		"sudo -n env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C apt-get update",
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
	assert.Contains(t, err.Error(), "expected")
	// The first iteration (sources.list slot, here /etc/passwd) was
	// rejected before any rm fired.
	assert.Empty(t, runner.captureCallCmds)
}

func TestPackageRepositoryInstaller_Remove_FixedOrderIgnoresStateOrder(t *testing.T) {
	t.Parallel()
	runner := &mockCommandRunner{}
	// A tampered/hand-edited state with the installed files in
	// reversed order must NOT cause Remove to delete the keyring
	// before the sources.list — apt would briefly see a sources.list
	// pointing at a missing keyring on concurrent traffic. Remove
	// uses the canonical [sources, keyring] order regardless of
	// state.InstalledFiles ordering.
	state := &resource.SystemPackageRepositoryState{
		InstalledFiles: []string{
			sourcesListPath("docker"), // reversed (canonical install order is [keyring, sources])
			keyringPath("docker"),
		},
	}
	err := New(runner).PackageRepositoryInstaller(&mockDownloader{}).Remove(context.Background(), state, "docker")
	require.NoError(t, err)
	require.Len(t, runner.captureCallCmds, 3)
	assert.Contains(t, runner.captureCallCmds[0][0], sourcesListPath("docker"), "sources.list must be removed first regardless of state order")
	assert.Contains(t, runner.captureCallCmds[1][0], keyringPath("docker"), "keyring must be removed second regardless of state order")
}

func TestPackageRepositoryInstaller_Remove_PathOtherRepositoryRejected(t *testing.T) {
	t.Parallel()
	runner := &mockCommandRunner{}
	// A tampered state file that points at another repository's keyring
	// (under the same allowed directory) must be refused — the security
	// gate is exact match against the deterministic paths for this name,
	// not just a directory-prefix check.
	state := &resource.SystemPackageRepositoryState{
		InstalledFiles: []string{
			keyringPath("victim"),
			sourcesListPath("victim"),
		},
	}
	err := New(runner).PackageRepositoryInstaller(&mockDownloader{}).Remove(context.Background(), state, "docker")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `for repository "docker"`)
	assert.Empty(t, runner.captureCallCmds, "no rm should fire when state points at another repository's files")
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

// TestFailedFetchCollector_DeduplicatesAndSorts pins the contract that
// urls() returns a deterministic, duplicate-free list. apt-get update
// can name the same failing URL on multiple lines (one per fetch
// stage — InRelease, Release, Release.gpg), and the collector
// receives lines from two goroutines (stdout + stderr) so append
// order is otherwise race-dependent. Dedupe + sort make the
// user-facing error string stable across runs.
func TestFailedFetchCollector_DeduplicatesAndSorts(t *testing.T) {
	t.Parallel()
	c := newFailedFetchCollector("https://example.com/repo")
	c.scanLine("W: Failed to fetch https://example.com/repo/dists/jammy/Release  404")
	c.scanLine("W: Failed to fetch https://example.com/repo/dists/jammy/Release  404")
	c.scanLine("W: Failed to fetch https://example.com/repo/dists/jammy/InRelease  404")
	got := c.urls()
	want := []string{
		"https://example.com/repo/dists/jammy/InRelease",
		"https://example.com/repo/dists/jammy/Release",
	}
	assert.Equal(t, want, got)
}

// TestAllowedAptOptions_CUEMatchesGo is the drift-detector: the CUE
// schema's #AptSource.options key constraint (a regex alternation in
// cuemodule/schema/schema.cue) must contain exactly the same key set
// the Go side accepts (resource.AllowedAptOptionKeys). The CUE
// constraint moves the gate to `tomei validate`; the Go map enforces
// it at install time. If the two drift, `tomei validate` and
// `tomei apply` would disagree on which option keys are allowed — a
// latent correctness/security bug.
//
// The check reads the CUE file as text (no full CUE parser dep) and
// pulls the alternation between "^(" and ")$" out of the line declaring
// the options field. This is cheaper than embedding cuelang and pins the
// invariant well enough that intentional changes have to touch both files.
func TestAllowedAptOptions_CUEMatchesGo(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "cuemodule", "schema", "schema.cue"))
	require.NoError(t, err, "read schema.cue")
	const (
		marker = `options?: {[=~"^(`
		end    = `)$"]: string}`
	)
	idx := strings.Index(string(data), marker)
	require.NotEqual(t, -1, idx, "could not locate AptSource options regex in schema.cue — drift-detector wiring is broken; update the marker constant")
	rest := string(data)[idx+len(marker):]
	endIdx := strings.Index(rest, end)
	require.NotEqual(t, -1, endIdx, "could not locate end of AptSource options regex")
	cueKeys := strings.Split(rest[:endIdx], "|")
	cueSet := make(map[string]struct{}, len(cueKeys))
	for _, k := range cueKeys {
		cueSet[k] = struct{}{}
	}
	// Go-side allowlist must match the CUE alternation exactly.
	goKeys := resource.AllowedAptOptionKeys()
	goSet := make(map[string]struct{}, len(goKeys))
	for _, k := range goKeys {
		goSet[k] = struct{}{}
		if _, ok := cueSet[k]; !ok {
			t.Errorf("Go allowlist has key %q absent from CUE alternation in schema.cue", k)
		}
	}
	for k := range cueSet {
		if _, ok := goSet[k]; !ok {
			t.Errorf("CUE alternation in schema.cue has key %q absent from Go allowlist", k)
		}
	}
}
