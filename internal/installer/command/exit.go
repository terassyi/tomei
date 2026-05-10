package command

import (
	"errors"
	"os/exec"
)

// IsExitCode reports whether err's chain contains an *exec.ExitError with
// the given exit code. Returns false if err is nil or does not unwrap to
// a *exec.ExitError.
//
// Executor.ExecuteCapture / ExecuteWithOutput preserve the underlying
// *exec.ExitError via fmt.Errorf("...: %w", ...), so callers can pass the
// wrapped error directly.
//
// This accessor exists so installer-layer packages (e.g. internal/installer/apt)
// can branch on specific exit codes without importing os/exec themselves —
// keeping the os/exec dependency localized to the command package and
// making the CommandRunner abstraction's contract simpler.
func IsExitCode(err error, code int) bool {
	if err == nil {
		return false
	}
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == code
}
