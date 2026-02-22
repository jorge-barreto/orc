package dispatch

import (
	"errors"
	"os/exec"
)

// exitCode extracts an exit code from a command error.
// Returns (code, nil) for ExitError, (0, err) for other errors, (0, nil) for nil.
func exitCode(err error) (int, error) {
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return 0, err
}
