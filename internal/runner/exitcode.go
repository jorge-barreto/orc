package runner

import "errors"

// Exit codes for orc run.
const (
	ExitSuccess     = 0   // Workflow completed successfully
	ExitRetryable   = 1   // Retryable failure (agent fail, loop max, timeout)
	ExitHumanNeeded = 2   // Human intervention needed (gate denied)
	ExitConfigError = 3   // Configuration or setup error
	ExitSignal      = 130 // Signal interrupt (SIGINT/SIGTERM)
)

// ExitError wraps an error with an orc exit code.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	return e.Err.Error()
}

func (e *ExitError) Unwrap() error {
	return e.Err
}

// ExitCodeFrom extracts the orc exit code from an error.
// Returns ExitSuccess (0) for nil, the embedded code for *ExitError,
// or ExitRetryable (1) for any other error.
func ExitCodeFrom(err error) int {
	if err == nil {
		return ExitSuccess
	}
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}
	return ExitRetryable
}
