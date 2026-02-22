package dispatch

import (
	"fmt"
	"os/exec"
	"testing"
)

func TestExitCode_Nil(t *testing.T) {
	code, err := exitCode(nil)
	if code != 0 || err != nil {
		t.Fatalf("code=%d, err=%v", code, err)
	}
}

func TestExitCode_OtherError(t *testing.T) {
	code, err := exitCode(fmt.Errorf("some error"))
	if code != 0 || err == nil {
		t.Fatalf("code=%d, err=%v", code, err)
	}
}

func TestExitCode_ExitError(t *testing.T) {
	// Run a command that exits with code 42
	cmd := exec.Command("bash", "-c", "exit 42")
	runErr := cmd.Run()

	code, err := exitCode(runErr)
	if code != 42 || err != nil {
		t.Fatalf("code=%d, err=%v", code, err)
	}
}
