package ux

import (
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
