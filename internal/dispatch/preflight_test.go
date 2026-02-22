package dispatch

import (
	"strings"
	"testing"

	"github.com/jorge-barreto/orc/internal/config"
)

func TestPreflight_BashFound(t *testing.T) {
	phases := []config.Phase{
		{Name: "a", Type: "script", Run: "echo hello"},
	}
	if err := Preflight(phases); err != nil {
		t.Fatalf("expected bash to be found, got: %v", err)
	}
}

func TestPreflight_GateNoBinariesNeeded(t *testing.T) {
	phases := []config.Phase{
		{Name: "approve", Type: "gate"},
	}
	if err := Preflight(phases); err != nil {
		t.Fatalf("gate-only phases should need no binaries, got: %v", err)
	}
}

func TestPreflight_MissingBinary(t *testing.T) {
	// We can't easily make "bash" disappear, but we can test the code path
	// by checking that a workflow needing a nonexistent binary fails.
	// We'll temporarily abuse the agent type since "claude" is unlikely in CI.
	phases := []config.Phase{
		{Name: "a", Type: "agent", Prompt: "test.md"},
	}
	err := Preflight(phases)
	if err == nil {
		// claude is on PATH — skip this test
		t.Skip("claude binary found on PATH, cannot test missing binary path")
	}
	if !strings.Contains(err.Error(), "claude") {
		t.Fatalf("expected error mentioning claude, got: %v", err)
	}
}
