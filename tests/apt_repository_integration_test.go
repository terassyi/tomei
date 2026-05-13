//go:build integration

package tests

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/tomei/internal/installer/apt"
	"github.com/terassyi/tomei/internal/installer/command"
	"github.com/terassyi/tomei/internal/installer/download"
	"github.com/terassyi/tomei/internal/resource"
)

// keyHashSHA256 must match the SHA256 of
// internal/installer/apt/testdata/tomei-integration-test.asc. If the
// fixture is regenerated, update this constant in lockstep — see
// testdata/README.md for the procedure.
const keyHashSHA256 = "sha256:c136badca3a932d7d4ae3a48068370d203ae3fb876dec76fa81ef0ecf4ef32bf"

// TestPackageRepositoryInstaller_RealSystem_RollbackOnUpdateFailure
// exercises Install end-to-end against the real local filesystem, real
// sudo, real gpg --dearmor, and real apt-get update — but with a
// localhost httptest server standing in for the third-party APT
// repository. The server returns the test fixture key for /key.asc and
// 404 for every other path, so apt-get update emits
// `W: Failed to fetch <localhost>/dists/...` against the configured
// sources.list, the partial-fetch detector fires, and Install rolls
// back the just-placed keyring and sources.list before returning an
// error.
//
// What this verifies in a way unit tests cannot:
//
//   - download.NewDownloader actually fetches the armored key over
//     HTTP from the httptest server (the validator allows
//     http://127.0.0.1 / localhost for tests).
//   - The committed fixture key dearmors successfully through the real
//     /usr/bin/gpg binary.
//   - `sudo -n install -D -m 0644 -o root -g root --` actually creates
//     /usr/share/keyrings/<name>.gpg and /etc/apt/sources.list.d/<name>.list
//     as root:root with 0644 permissions.
//   - `apt-get update` is invoked with stderr-on-stdout merge so
//     `W: Failed to fetch` warnings reach the helper.
//   - The rollback path actually removes both files via real sudo rm.
//
// Coverage gap: the apt-get update **success** path is not exercised by
// this test (the localhost server cannot pretend to be a fully-signed
// APT repository). Successful Install / Remove ordering is covered by
// the unit tests in internal/installer/apt/repository_test.go.
//
// Requires Linux + gpg + apt-get + sudo + passwordless sudo. Skips on
// any other configuration.
func TestPackageRepositoryInstaller_RealSystem_RollbackOnUpdateFailure(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("apt repository integration test requires Linux")
	}
	for _, bin := range []string{"apt-get", "dpkg", "sudo", "gpg"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not found in PATH", bin)
		}
	}
	if err := exec.Command("sudo", "-n", "true").Run(); err != nil {
		t.Skip("passwordless sudo not available")
	}

	// Load fixture key. Path relative to the tests/ package is the
	// repo-relative path because go test sets working dir to the package
	// directory.
	armored, err := os.ReadFile(filepath.Join("..", "internal", "installer", "apt", "testdata", "tomei-integration-test.asc"))
	require.NoError(t, err, "fixture key must be present at testdata/tomei-integration-test.asc")

	// Each test run uses a unique repo name so the test is safe to run
	// repeatedly on a host where a previous run was interrupted before
	// cleanup, and so it never collides with anything a developer might
	// have installed manually. The name is also constrained to the same
	// CUE-side regex (lowercase + dot/underscore/hyphen) that production
	// would enforce.
	repoName := fmt.Sprintf("tomei-integration-test-%d", os.Getpid())

	keyringPath := filepath.Join("/usr/share/keyrings", repoName+".gpg")
	sourcesPath := filepath.Join("/etc/apt/sources.list.d", repoName+".list")

	// Belt-and-suspenders cleanup. If Install rolls back successfully
	// these files won't exist; if it doesn't, this cleans up the host so
	// a developer doesn't have to.
	t.Cleanup(func() {
		for _, p := range []string{sourcesPath, keyringPath} {
			cmd := exec.Command("sudo", "-n", "rm", "-f", "--", p)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Logf("cleanup: sudo rm -f %s: %v: %s", p, err, out)
			}
		}
		// Refresh the apt index so any lingering cache for the test repo
		// is dropped.
		_ = exec.Command("sudo", "-n", "env", "DEBIAN_FRONTEND=noninteractive",
			"apt-get", "update", "-o", "DPkg::Lock::Timeout=60").Run()
	})

	// httptest server: serve the armored key for /key.asc, 404 for
	// every other path. The 404 on /dists/... is precisely what
	// produces the `W: Failed to fetch` warning that the rollback path
	// reacts to.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/key.asc" {
			w.Header().Set("Content-Type", "application/pgp-keys")
			_, _ = w.Write(armored)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	res := &resource.SystemPackageRepository{
		BaseResource: resource.BaseResource{
			APIVersion:   "tomei.terassyi.net/v1beta1",
			ResourceKind: resource.KindSystemPackageRepository,
			Metadata:     resource.Metadata{Name: repoName},
		},
		SystemPackageRepositorySpec: &resource.SystemPackageRepositorySpec{
			InstallerRef: "apt",
			Apt: &resource.AptSource{
				URL:        server.URL,
				KeyURL:     server.URL + "/key.asc",
				KeyHash:    keyHashSHA256,
				Suite:      "stable",
				Components: []string{"main"},
				Options:    map[string]string{"arch": "amd64"},
			},
		},
	}

	client := apt.New(command.NewExecutor(""))
	installer := client.PackageRepositoryInstaller(download.NewDownloader())

	state, err := installer.Install(context.Background(), res, repoName)
	require.Error(t, err, "apt-get update against a non-repo localhost server must fail")
	assert.Nil(t, state, "no state should be returned when Install fails")
	assert.Contains(t, err.Error(), "failed to fetch",
		"the error should attribute the failure to a fetch problem, not a generic update error")
	assert.Contains(t, err.Error(), server.URL,
		"the error should name the URL whose fetch failed")

	// Both files must have been rolled back. Use stat via real exec
	// because tomei runs as a non-root user and /etc/apt/sources.list.d
	// is root-readable so a Go-level os.Stat works for absence checks,
	// but using `test -e` via sudo is more symmetric with how Install
	// places the files.
	for _, p := range []string{keyringPath, sourcesPath} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("expected %s to have been rolled back, but it still exists (stat err: %v)", p, err)
		}
	}
}
