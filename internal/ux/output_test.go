package ux

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jorge-barreto/orc/internal/config"
)

func TestWrapLines(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		maxWidth int
		want     []string
	}{
		{"short text", "hello world", 60, []string{"hello world"}},
		{"long text wraps", "the quick brown fox jumps over the lazy dog", 20, []string{
			"the quick brown fox",
			"jumps over the lazy",
			"dog",
		}},
		{"empty text", "", 60, []string{""}},
		{"single long word", "superlongword", 5, []string{"superlongword"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrapLines(tt.text, tt.maxWidth)
			if len(got) != len(tt.want) {
				t.Fatalf("wrapLines(%q, %d) returned %d lines, want %d: %v", tt.text, tt.maxWidth, len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("line %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestDisableColor(t *testing.T) {
	// Save originals
	origReset, origBold, origDim := Reset, Bold, Dim
	origRed, origGreen, origYellow := Red, Green, Yellow
	origCyan, origMagenta, origBlue := Cyan, Magenta, Blue
	origBoldCyan, origBoldBlue, origBoldGreen := BoldCyan, BoldBlue, BoldGreen

	t.Cleanup(func() {
		Reset, Bold, Dim = origReset, origBold, origDim
		Red, Green, Yellow = origRed, origGreen, origYellow
		Cyan, Magenta, Blue = origCyan, origMagenta, origBlue
		BoldCyan, BoldBlue, BoldGreen = origBoldCyan, origBoldBlue, origBoldGreen
	})

	// Pre-condition: vars are non-empty
	if Reset == "" {
		t.Fatal("expected Reset to be non-empty before DisableColor()")
	}

	DisableColor()

	for name, val := range map[string]string{
		"Reset": Reset, "Bold": Bold, "Dim": Dim,
		"Red": Red, "Green": Green, "Yellow": Yellow,
		"Cyan": Cyan, "Magenta": Magenta, "Blue": Blue,
		"BoldCyan": BoldCyan, "BoldBlue": BoldBlue, "BoldGreen": BoldGreen,
	} {
		if val != "" {
			t.Errorf("%s = %q after DisableColor(), want \"\"", name, val)
		}
	}
}

func TestIsTerminal_Pipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	if IsTerminal(r) {
		t.Error("IsTerminal(pipe reader) = true, want false")
	}
}

func TestEnableQuiet_SetsQuietModeAndDisablesColor(t *testing.T) {
	origQuiet := QuietMode
	origReset, origBold, origDim := Reset, Bold, Dim
	origRed, origGreen, origYellow := Red, Green, Yellow
	origCyan, origMagenta, origBlue := Cyan, Magenta, Blue
	origBoldCyan, origBoldBlue, origBoldGreen := BoldCyan, BoldBlue, BoldGreen

	t.Cleanup(func() {
		QuietMode = origQuiet
		Reset, Bold, Dim = origReset, origBold, origDim
		Red, Green, Yellow = origRed, origGreen, origYellow
		Cyan, Magenta, Blue = origCyan, origMagenta, origBlue
		BoldCyan, BoldBlue, BoldGreen = origBoldCyan, origBoldBlue, origBoldGreen
	})

	EnableQuiet()

	if !QuietMode {
		t.Error("QuietMode should be true after EnableQuiet()")
	}
	for name, val := range map[string]string{
		"Reset": Reset, "Bold": Bold, "Dim": Dim,
		"Red": Red, "Green": Green, "Yellow": Yellow,
		"Cyan": Cyan, "Magenta": Magenta, "Blue": Blue,
		"BoldCyan": BoldCyan, "BoldBlue": BoldBlue, "BoldGreen": BoldGreen,
	} {
		if val != "" {
			t.Errorf("%s = %q after EnableQuiet(), want \"\"", name, val)
		}
	}
}

func TestQuietPhaseEvent_ValidJSON(t *testing.T) {
	// Basic event — no extra fields
	out := captureOutput(func() {
		QuietPhaseEvent("plan", "started", nil)
	})
	out = strings.TrimSpace(out)
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(out), &event); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}
	if event["phase"] != "plan" {
		t.Errorf("phase = %v, want \"plan\"", event["phase"])
	}
	if event["status"] != "started" {
		t.Errorf("status = %v, want \"started\"", event["status"])
	}

	// Event with extra fields
	out2 := captureOutput(func() {
		QuietPhaseEvent("plan", "complete", map[string]interface{}{"duration_s": 120.5})
	})
	out2 = strings.TrimSpace(out2)
	var event2 map[string]interface{}
	if err := json.Unmarshal([]byte(out2), &event2); err != nil {
		t.Fatalf("output with extra is not valid JSON: %v\noutput: %s", err, out2)
	}
	if event2["duration_s"] != 120.5 {
		t.Errorf("duration_s = %v, want 120.5", event2["duration_s"])
	}
}

func TestQuietPhaseEvent_ConcurrentWrites(t *testing.T) {
	origQuiet := QuietMode
	t.Cleanup(func() { QuietMode = origQuiet })
	QuietMode = true

	origStdout := os.Stdout
	rp, wp, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = wp
	defer wp.Close()
	t.Cleanup(func() { os.Stdout = origStdout })

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			QuietPhaseEvent(fmt.Sprintf("phase-%d", idx), "started", nil)
		}(i)
	}
	wg.Wait()
	wp.Close()

	var buf bytes.Buffer
	io.Copy(&buf, rp)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != n {
		t.Fatalf("expected %d JSON lines, got %d", n, len(lines))
	}
	for i, line := range lines {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("line %d not valid JSON: %q: %v", i, line, err)
		}
	}
}

func TestPhaseHeader_QuietMode_EmitsJSONLine(t *testing.T) {
	origQuiet := QuietMode
	t.Cleanup(func() { QuietMode = origQuiet })
	QuietMode = true

	out := captureOutput(func() {
		PhaseHeader(0, 3, config.Phase{Name: "plan", Type: "agent"})
	})
	out = strings.TrimSpace(out)
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(out), &event); err != nil {
		t.Fatalf("PhaseHeader quiet output is not valid JSON: %v\noutput: %s", err, out)
	}
	if event["phase"] != "plan" {
		t.Errorf("phase = %v, want \"plan\"", event["phase"])
	}
	if event["status"] != "started" {
		t.Errorf("status = %v, want \"started\"", event["status"])
	}
}
