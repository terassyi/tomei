package command

import (
	"errors"
	"fmt"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// realExitError builds a real *exec.ExitError via `sh -c 'exit N'`.
// Linux/macOS/POSIX sh required; Windows is unsupported (tomei's CI is
// ubuntu-latest only). Duplicated in apt_test.go because Go's _test.go
// scoping prevents test helpers from being shared across package
// boundaries — the 5-line cost is preferable to introducing an
// infrastructure-only `cmdtest` subpackage.
func realExitError(t *testing.T, code int) error {
	t.Helper()
	err := exec.Command("sh", "-c", fmt.Sprintf("exit %d", code)).Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	return err
}

func TestIsExitCode(t *testing.T) {
	t.Parallel()

	t.Run("matching exit code through wrap", func(t *testing.T) {
		t.Parallel()
		raw := realExitError(t, 1)
		wrapped := fmt.Errorf("command failed: x: %w", raw)
		assert.True(t, IsExitCode(wrapped, 1))
		assert.False(t, IsExitCode(wrapped, 2))
	})
	t.Run("non-matching exit code (raw)", func(t *testing.T) {
		t.Parallel()
		raw := realExitError(t, 3)
		assert.False(t, IsExitCode(raw, 1))
		assert.True(t, IsExitCode(raw, 3))
	})
	t.Run("nil error", func(t *testing.T) {
		t.Parallel()
		assert.False(t, IsExitCode(nil, 1))
	})
	t.Run("non-ExitError", func(t *testing.T) {
		t.Parallel()
		assert.False(t, IsExitCode(errors.New("plain error"), 1))
	})
}
