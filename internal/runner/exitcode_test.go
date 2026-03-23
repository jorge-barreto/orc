package runner

import (
	"errors"
	"fmt"
	"testing"
)

func TestExitCodeFrom_Nil(t *testing.T) {
	if code := ExitCodeFrom(nil); code != ExitSuccess {
		t.Fatalf("ExitCodeFrom(nil) = %d, want %d", code, ExitSuccess)
	}
}

func TestExitCodeFrom_ExitError(t *testing.T) {
	for _, code := range []int{ExitPhaseFailure, ExitTimeout, ExitCostLimit, ExitInterrupted, ExitResumeFailure} {
		err := &ExitError{Code: code, Err: fmt.Errorf("test")}
		if got := ExitCodeFrom(err); got != code {
			t.Fatalf("ExitCodeFrom(ExitError{%d}) = %d", code, got)
		}
	}
}

func TestExitCodeFrom_WrappedExitError(t *testing.T) {
	inner := &ExitError{Code: ExitCostLimit, Err: fmt.Errorf("gate denied")}
	wrapped := fmt.Errorf("run failed: %w", inner)
	if code := ExitCodeFrom(wrapped); code != ExitCostLimit {
		t.Fatalf("ExitCodeFrom(wrapped) = %d, want %d", code, ExitCostLimit)
	}
}

func TestExitCodeFrom_PlainError(t *testing.T) {
	err := fmt.Errorf("some error")
	if code := ExitCodeFrom(err); code != ExitPhaseFailure {
		t.Fatalf("ExitCodeFrom(plain) = %d, want %d", code, ExitPhaseFailure)
	}
}

func TestExitError_Error(t *testing.T) {
	err := &ExitError{Code: ExitConfigError, Err: fmt.Errorf("bad config")}
	if err.Error() != "bad config" {
		t.Fatalf("Error() = %q", err.Error())
	}
}

func TestExitError_Unwrap(t *testing.T) {
	inner := fmt.Errorf("inner")
	err := &ExitError{Code: ExitPhaseFailure, Err: inner}
	if !errors.Is(err, inner) {
		t.Fatal("Unwrap did not return inner error")
	}
}
