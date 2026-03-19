package ux

import (
	"testing"
)

func TestParseStepInput(t *testing.T) {
	tests := []struct {
		input      string
		wantType   string
		wantTarget string
	}{
		{"", "continue", ""},
		{"c", "continue", ""},
		{"continue", "continue", ""},
		{"a", "abort", ""},
		{"abort", "abort", ""},
		{"r 3", "rewind", "3"},
		{"rewind implement", "rewind", "implement"},
		{"r", "unknown", ""},
		{"i plan.md", "inspect", "plan.md"},
		{"inspect design.md", "inspect", "design.md"},
		{"i", "unknown", ""},
		{"xyz", "unknown", ""},
		{"C", "continue", ""},
		{"A", "abort", ""},
		{"R 3", "rewind", "3"},
		{"I file", "inspect", "file"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseStepInput(tt.input)
			if got.Type != tt.wantType {
				t.Errorf("ParseStepInput(%q).Type = %q, want %q", tt.input, got.Type, tt.wantType)
			}
			if got.Target != tt.wantTarget {
				t.Errorf("ParseStepInput(%q).Target = %q, want %q", tt.input, got.Target, tt.wantTarget)
			}
		})
	}
}

func TestSafeArtifactPath(t *testing.T) {
	tests := []struct {
		artifactsDir string
		target       string
		wantPath     string
		wantErr      bool
	}{
		{"/tmp/art", "plan.md", "/tmp/art/plan.md", false},
		{"/tmp/art", "../../../etc/passwd", "", true},
		{"/tmp/art", "logs/phase-1.log", "/tmp/art/logs/phase-1.log", false},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			got, err := safeArtifactPath(tt.artifactsDir, tt.target)
			if (err != nil) != tt.wantErr {
				t.Errorf("safeArtifactPath(%q, %q) error = %v, wantErr %v", tt.artifactsDir, tt.target, err, tt.wantErr)
			}
			if got != tt.wantPath {
				t.Errorf("safeArtifactPath(%q, %q) = %q, want %q", tt.artifactsDir, tt.target, got, tt.wantPath)
			}
		})
	}
}
