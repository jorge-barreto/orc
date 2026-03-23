package ux

import (
	"os"
	"testing"
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
