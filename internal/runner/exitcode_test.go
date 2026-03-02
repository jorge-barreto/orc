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
	for _, code := range []int{ExitRetryable, ExitHumanNeeded, ExitConfigError, ExitSignal} {
		err := &ExitError{Code: code, Err: fmt.Errorf("test")}
		if got := ExitCodeFrom(err); got != code {
			t.Fatalf("ExitCodeFrom(ExitError{%d}) = %d", code, got)
		}
	}
}

func TestExitCodeFrom_WrappedExitError(t *testing.T) {
	inner := &ExitError{Code: ExitHumanNeeded, Err: fmt.Errorf("gate denied")}
	wrapped := fmt.Errorf("run failed: %w", inner)
	if code := ExitCodeFrom(wrapped); code != ExitHumanNeeded {
		t.Fatalf("ExitCodeFrom(wrapped) = %d, want %d", code, ExitHumanNeeded)
	}
}

func TestExitCodeFrom_PlainError(t *testing.T) {
	err := fmt.Errorf("some error")
	if code := ExitCodeFrom(err); code != ExitRetryable {
		t.Fatalf("ExitCodeFrom(plain) = %d, want %d", code, ExitRetryable)
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
	err := &ExitError{Code: ExitRetryable, Err: inner}
	if !errors.Is(err, inner) {
		t.Fatal("Unwrap did not return inner error")
	}
}
