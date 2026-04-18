//go:build e2e

package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestHeadless_JSONLEvents verifies that --headless emits structured
// JSONL events (phase started/completed) with no ANSI escape codes, and
// that --headless implies --auto (gate phases are auto-approved).
func TestHeadless_JSONLEvents(t *testing.T) {
	cfg := `name: headless
phases:
  - name: first
    type: script
    run: echo hello

  - name: approve
    type: gate
    description: headless should auto-approve

  - name: second
    type: script
    run: echo world
`
	w := NewWorkspace(t, cfg)
	w.Ticket = "HEADLESS-001"
	w.ArtifactsDir = strings.Replace(w.ArtifactsDir, "TEST-1", "HEADLESS-001", 1)

	res := w.RunOrc("run", w.Ticket, "--headless")

	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout: %s\nstderr: %s",
			res.ExitCode, res.Stdout, res.Stderr)
	}

	// No ANSI escape sequences in stdout.
	if strings.Contains(res.Stdout, "\x1b[") {
		t.Errorf("stdout contains ANSI escape; headless should disable color\nstdout: %s", res.Stdout)
	}

	// Parse stdout line-by-line; collect phase-event lines.
	var phaseEvents []map[string]any
	var nonJSONLines []string
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			nonJSONLines = append(nonJSONLines, line)
			continue
		}
		if _, hasPhase := obj["phase"]; hasPhase {
			phaseEvents = append(phaseEvents, obj)
		}
	}

	// We expect at minimum a started event and some kind of completion for
	// each of the 3 phases. Gate auto-approval may emit its own event.
	// Don't assert exact count — assert each phase name appears at least
	// once as a JSON event.
	seen := map[string]bool{}
	for _, e := range phaseEvents {
		if name, _ := e["phase"].(string); name != "" {
			seen[name] = true
		}
	}
	for _, want := range []string{"first", "approve", "second"} {
		if !seen[want] {
			t.Errorf("no JSON phase event for %q; events=%v", want, phaseEvents)
		}
	}

	// nonJSONLines is OK to contain script stdout ("hello", "world") —
	// the orc control output is separate from child stdout. Document:
	// do NOT assert "every line is JSON" because that's not actually true
	// (scripts write to the shared stdout). Assert instead: the orc
	// event stream IS JSON, and ANSI is absent.

	// Run completed in state.
	rr := w.ReadRunResult()
	if got := rr["status"]; got != "completed" {
		t.Errorf("run-result.status = %v, want \"completed\"", got)
	}

	// All 3 phases completed (gate must have auto-approved).
	phases, _ := rr["phases"].([]any)
	if len(phases) != 3 {
		t.Fatalf("phases len = %d, want 3", len(phases))
	}
	for i, p := range phases {
		m, _ := p.(map[string]any)
		if got := m["status"]; got != "completed" {
			t.Errorf("phase[%d] (%v).status = %v, want \"completed\"",
				i, m["name"], got)
		}
	}
}
