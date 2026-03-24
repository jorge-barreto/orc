package ux

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/state"
)

func makeTiming(entries []state.TimingEntry) *state.Timing {
	return state.NewTiming(entries)
}

func captureOutput(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestFmtDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "<1s"},
		{500 * time.Millisecond, "<1s"},
		{5 * time.Second, "5s"},
		{38 * time.Second, "38s"},
		{72 * time.Second, "1m 12s"},
		{270 * time.Second, "4m 30s"},
	}
	for _, tt := range tests {
		got := fmtDuration(tt.d)
		if got != tt.want {
			t.Errorf("fmtDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestCountRuns(t *testing.T) {
	// Nil timing
	if got := countRuns(nil, "any"); got != 0 {
		t.Errorf("countRuns(nil, ...) = %d, want 0", got)
	}

	now := time.Now()
	timing := makeTiming([]state.TimingEntry{
		{Phase: "implement", Start: now, End: now.Add(10 * time.Second)},
		{Phase: "plan", Start: now, End: now.Add(5 * time.Second)},
		{Phase: "implement", Start: now, End: now.Add(15 * time.Second)},
		{Phase: "implement", Start: now, End: now.Add(20 * time.Second)},
	})

	if got := countRuns(timing, "implement"); got != 3 {
		t.Errorf("countRuns(timing, implement) = %d, want 3", got)
	}
	if got := countRuns(timing, "plan"); got != 1 {
		t.Errorf("countRuns(timing, plan) = %d, want 1", got)
	}
	if got := countRuns(timing, "nonexistent"); got != 0 {
		t.Errorf("countRuns(timing, nonexistent) = %d, want 0", got)
	}
}

func TestLastDuration(t *testing.T) {
	// Nil timing
	if got := lastDuration(nil, "any"); got != 0 {
		t.Errorf("lastDuration(nil, ...) = %v, want 0", got)
	}

	now := time.Now()
	timing := makeTiming([]state.TimingEntry{
		{Phase: "implement", Start: now, End: now.Add(10 * time.Second)},
		{Phase: "implement", Start: now, End: now.Add(25 * time.Second)},
	})

	got := lastDuration(timing, "implement")
	if got != 25*time.Second {
		t.Errorf("lastDuration = %v, want 25s", got)
	}

	// Entry without End (End.IsZero()) should be skipped
	timing2 := makeTiming([]state.TimingEntry{
		{Phase: "test", Start: now, End: now.Add(8 * time.Second)},
		{Phase: "test", Start: now}, // no End
	})

	got2 := lastDuration(timing2, "test")
	if got2 != 8*time.Second {
		t.Errorf("lastDuration with zero end = %v, want 8s", got2)
	}
}

func TestRunSummary_AllPass(t *testing.T) {
	phases := []config.Phase{
		{Name: "plan", Type: "agent"},
		{Name: "implement", Type: "agent"},
		{Name: "test", Type: "script"},
	}

	now := time.Now()
	timing := makeTiming([]state.TimingEntry{
		{Phase: "plan", Start: now, End: now.Add(72 * time.Second)},
		{Phase: "implement", Start: now, End: now.Add(120 * time.Second)},
		{Phase: "test", Start: now, End: now.Add(5 * time.Second)},
	})

	output := captureOutput(func() {
		RunSummary(phases, timing, -1, nil)
	})

	checks := []string{
		"Run complete",
		"3 phases",
		"plan",
		"implement",
		"test",
		"pass",
		"1m 12s",
		"2m 00s",
		"5s",
		"Total agent time:",
		"Total script time:",
	}
	for _, s := range checks {
		if !strings.Contains(output, s) {
			t.Errorf("output missing %q\nfull output:\n%s", s, output)
		}
	}

	// Verify run count "1" appears (for each phase)
	// Count "  1 " patterns won't work cleanly, just verify "1" is in run columns
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "plan") && strings.Contains(line, "agent") {
			if !strings.Contains(line, "pass") {
				t.Errorf("plan line should show pass: %s", line)
			}
		}
	}
}

func TestRunSummary_WithRetries(t *testing.T) {
	phases := []config.Phase{
		{Name: "plan", Type: "agent"},
		{Name: "implement", Type: "agent"},
		{Name: "test", Type: "script"},
	}

	now := time.Now()
	timing := makeTiming([]state.TimingEntry{
		// plan: 1 run
		{Phase: "plan", Start: now, End: now.Add(72 * time.Second)},
		// implement: 3 runs (original + 2 re-executions)
		{Phase: "implement", Start: now, End: now.Add(60 * time.Second)},
		// test: first fail
		{Phase: "test", Start: now, End: now.Add(5 * time.Second)},
		// implement re-run 2
		{Phase: "implement", Start: now, End: now.Add(45 * time.Second)},
		// test: second fail
		{Phase: "test", Start: now, End: now.Add(4 * time.Second)},
		// implement re-run 3
		{Phase: "implement", Start: now, End: now.Add(50 * time.Second)},
		// test: passes
		{Phase: "test", Start: now, End: now.Add(3 * time.Second)},
	})

	output := captureOutput(func() {
		RunSummary(phases, timing, -1, nil)
	})

	if !strings.Contains(output, "Run complete") {
		t.Errorf("expected 'Run complete' in output:\n%s", output)
	}

	// Check run counts
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "plan") && strings.Contains(line, "agent") {
			if !strings.Contains(line, "    1") {
				t.Errorf("plan should show 1 run: %s", line)
			}
		}
		if strings.Contains(line, "implement") && strings.Contains(line, "agent") {
			if !strings.Contains(line, "    3") {
				t.Errorf("implement should show 3 runs: %s", line)
			}
		}
		if strings.Contains(line, "test") && strings.Contains(line, "script") {
			if !strings.Contains(line, "    3") {
				t.Errorf("test should show 3 runs: %s", line)
			}
		}
	}

	// All should show pass
	if strings.Contains(output, "FAIL") {
		t.Errorf("no phase should show FAIL:\n%s", output)
	}
}

func TestRunSummary_WithFailure(t *testing.T) {
	phases := []config.Phase{
		{Name: "plan", Type: "agent"},
		{Name: "implement", Type: "agent"},
		{Name: "test", Type: "script"},
	}

	now := time.Now()
	timing := makeTiming([]state.TimingEntry{
		{Phase: "plan", Start: now, End: now.Add(72 * time.Second)},
		// implement: 3 runs
		{Phase: "implement", Start: now, End: now.Add(55 * time.Second)},
		{Phase: "test", Start: now, End: now.Add(3 * time.Second)},
		{Phase: "implement", Start: now, End: now.Add(50 * time.Second)},
		{Phase: "test", Start: now, End: now.Add(3 * time.Second)},
		{Phase: "implement", Start: now, End: now.Add(45 * time.Second)},
		// test: final fail
		{Phase: "test", Start: now, End: now.Add(2 * time.Second)},
	})

	output := captureOutput(func() {
		RunSummary(phases, timing, 2, nil)
	})

	if !strings.Contains(output, "Run failed") {
		t.Errorf("expected 'Run failed' in output:\n%s", output)
	}
	if !strings.Contains(output, "3 phases") {
		t.Errorf("expected '3 phases' in output:\n%s", output)
	}
	if !strings.Contains(output, "FAIL") {
		t.Errorf("expected 'FAIL' in output:\n%s", output)
	}

	// Plan and implement should show pass
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "plan") && strings.Contains(line, "agent") {
			if !strings.Contains(line, "pass") {
				t.Errorf("plan should show pass: %s", line)
			}
		}
		if strings.Contains(line, "implement") && strings.Contains(line, "agent") {
			if !strings.Contains(line, "pass") {
				t.Errorf("implement should show pass: %s", line)
			}
		}
	}
}

func TestRunSummary_WithSkippedPhases(t *testing.T) {
	phases := []config.Phase{
		{Name: "plan", Type: "agent"},
		{Name: "optional-lint", Type: "script"},
		{Name: "implement", Type: "agent"},
		{Name: "test", Type: "script"},
	}

	now := time.Now()
	timing := makeTiming([]state.TimingEntry{
		{Phase: "plan", Start: now, End: now.Add(72 * time.Second)},
		{Phase: "implement", Start: now, End: now.Add(120 * time.Second)},
		{Phase: "test", Start: now, End: now.Add(5 * time.Second)},
	})

	skipped := map[string]bool{"optional-lint": true}

	output := captureOutput(func() {
		RunSummary(phases, timing, -1, skipped)
	})

	if !strings.Contains(output, "Run complete") {
		t.Errorf("expected 'Run complete':\n%s", output)
	}
	if !strings.Contains(output, "4 phases") {
		t.Errorf("expected '4 phases':\n%s", output)
	}
	if !strings.Contains(output, "skip") {
		t.Errorf("expected 'skip' for optional-lint:\n%s", output)
	}

	// Check that skipped phase shows dashes
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "optional-lint") {
			if !strings.Contains(line, "—") {
				t.Errorf("optional-lint should show — for runs/duration: %s", line)
			}
		}
	}
}

func TestRunSummary_PhantomPhasesHidden(t *testing.T) {
	phases := []config.Phase{
		{Name: "plan", Type: "agent"},
		{Name: "implement", Type: "agent"},
		{Name: "test", Type: "script"},
		{Name: "review", Type: "agent"},
		{Name: "ship", Type: "script"},
	}

	now := time.Now()
	timing := makeTiming([]state.TimingEntry{
		{Phase: "plan", Start: now, End: now.Add(72 * time.Second)},
		{Phase: "implement", Start: now, End: now.Add(55 * time.Second)},
		{Phase: "test", Start: now, End: now.Add(8 * time.Second)},
	})

	// Test with failedPhase=2 (test failed)
	output := captureOutput(func() {
		RunSummary(phases, timing, 2, nil)
	})

	if strings.Contains(output, "review") {
		t.Errorf("phantom phase 'review' should NOT appear:\n%s", output)
	}
	if strings.Contains(output, "ship") {
		t.Errorf("phantom phase 'ship' should NOT appear:\n%s", output)
	}
	if !strings.Contains(output, "plan") {
		t.Errorf("'plan' should appear:\n%s", output)
	}
	if !strings.Contains(output, "implement") {
		t.Errorf("'implement' should appear:\n%s", output)
	}
	if !strings.Contains(output, "test") {
		t.Errorf("'test' should appear:\n%s", output)
	}

	// Also test with failedPhase=-1 (interrupted between phases)
	output2 := captureOutput(func() {
		RunSummary(phases, timing, -1, nil)
	})

	if strings.Contains(output2, "review") {
		t.Errorf("phantom phase 'review' should NOT appear with failedPhase=-1:\n%s", output2)
	}
	if strings.Contains(output2, "ship") {
		t.Errorf("phantom phase 'ship' should NOT appear with failedPhase=-1:\n%s", output2)
	}
}

func TestRunSummary_QuietSuppressed(t *testing.T) {
	origQuiet := QuietMode
	t.Cleanup(func() { QuietMode = origQuiet })
	QuietMode = true

	phases := []config.Phase{
		{Name: "plan", Type: "agent"},
		{Name: "implement", Type: "agent"},
	}
	now := time.Now()
	timing := makeTiming([]state.TimingEntry{
		{Phase: "plan", Start: now, End: now.Add(10 * time.Second)},
		{Phase: "implement", Start: now, End: now.Add(20 * time.Second)},
	})

	out := captureOutput(func() {
		RunSummary(phases, timing, -1, nil)
	})
	if out != "" {
		t.Errorf("RunSummary should produce no output in quiet mode, got: %q", out)
	}
}

func TestRunSummary_ColumnAlignment(t *testing.T) {
	phases := []config.Phase{
		{Name: "a-very-long-phase-nm", Type: "agent"}, // 20 chars
		{Name: "b", Type: "script"},
	}

	now := time.Now()
	timing := makeTiming([]state.TimingEntry{
		{Phase: "a-very-long-phase-nm", Start: now, End: now.Add(10 * time.Second)},
		{Phase: "b", Start: now, End: now.Add(3 * time.Second)},
	})

	output := captureOutput(func() {
		RunSummary(phases, timing, -1, nil)
	})

	lines := strings.Split(output, "\n")

	// Find the header line and data lines
	var headerLine string
	var dataLines []string
	for _, line := range lines {
		if strings.Contains(line, "Phase") && strings.Contains(line, "Type") && strings.Contains(line, "Runs") {
			headerLine = line
		}
		if strings.Contains(line, "a-very-long-phase-nm") || (strings.Contains(line, " b ") && strings.Contains(line, "script")) {
			dataLines = append(dataLines, line)
		}
	}

	if headerLine == "" {
		t.Fatalf("header line not found in output:\n%s", output)
	}

	// Check that "Type" column starts at the same position in header and data lines
	headerTypeIdx := strings.Index(headerLine, "Type")
	if headerTypeIdx < 0 {
		t.Fatalf("'Type' not found in header: %s", headerLine)
	}

	for _, dl := range dataLines {
		// Find the type value (agent or script) — it should start at the same column
		agentIdx := strings.Index(dl, "agent")
		scriptIdx := strings.Index(dl, "script")
		typeIdx := agentIdx
		if typeIdx < 0 {
			typeIdx = scriptIdx
		}
		if typeIdx != headerTypeIdx {
			t.Errorf("column misalignment: header Type at %d, data type at %d\nheader: %s\ndata:   %s",
				headerTypeIdx, typeIdx, headerLine, dl)
		}
	}
}
