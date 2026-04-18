//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// TestSmoke_ScriptPhase runs a minimal workflow with one script phase.
// Proves the binary builds, the helper wires config + ticket correctly,
// and state.json lands where expected.
func TestSmoke_ScriptPhase(t *testing.T) {
	cfg := `name: smoke
phases:
  - name: say-hello
    type: script
    run: echo hello
`
	w := NewWorkspace(t, cfg)

	res := w.RunOrc("run", w.Ticket, "--auto")

	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout: %s\nstderr: %s",
			res.ExitCode, res.Stdout, res.Stderr)
	}

	rr := w.ReadRunResult()
	if got := rr["status"]; got != "completed" {
		t.Errorf("run-result.status = %v, want \"completed\"", got)
	}
	if got := rr["exit_code"]; got != float64(0) {
		t.Errorf("run-result.exit_code = %v, want 0", got)
	}

	state := w.ReadState()
	if got := state["status"]; got != "completed" {
		t.Errorf("state.status = %v, want \"completed\"", got)
	}

	combined := res.Stdout + res.Stderr
	if !strings.Contains(combined, "hello") {
		t.Errorf("expected 'hello' in output; got stdout=%q stderr=%q",
			res.Stdout, res.Stderr)
	}
}
