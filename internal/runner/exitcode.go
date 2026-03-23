package runner

import "errors"

// Exit codes for orc run.
const (
	ExitSuccess       = 0 // Workflow completed successfully
	ExitPhaseFailure  = 1 // Phase failure (agent fail, script fail, gate denied, loop exhaustion, missing outputs)
	ExitTimeout       = 2 // Phase timed out
	ExitConfigError   = 3 // Configuration or setup error
	ExitCostLimit     = 4 // Cost limit exceeded (per-phase or per-run)
	ExitInterrupted   = 5 // Signal interrupt (SIGINT/SIGTERM/SIGHUP)
	ExitResumeFailure = 6 // Cannot resume interrupted session

	// Deprecated: use ExitPhaseFailure (1).
	ExitRetryable = 1
	// Deprecated: use ExitPhaseFailure (1) for gate denial or ExitCostLimit (4) for cost limit.
	ExitHumanNeeded = 2
	// Deprecated: use ExitInterrupted (5).
	ExitSignal = 130
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
// or ExitPhaseFailure (1) for any other error.
func ExitCodeFrom(err error) int {
	if err == nil {
		return ExitSuccess
	}
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}
	return ExitPhaseFailure
}
