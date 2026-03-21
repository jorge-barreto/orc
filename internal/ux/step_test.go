package ux

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestListStepArtifacts_IncludesLog(t *testing.T) {
	tests := []struct {
		phaseIdx int
		logFile  string // relative to artifactsDir; empty = don't create
		wantLog  string // expected entry in result; empty = none expected
	}{
		{0, "logs/phase-1.log", "logs/phase-1.log"},
		{4, "logs/phase-5.log", "logs/phase-5.log"},
		{2, "", ""},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("phaseIdx=%d", tt.phaseIdx), func(t *testing.T) {
			dir := t.TempDir()
			if tt.logFile != "" {
				if err := os.MkdirAll(filepath.Join(dir, "logs"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, tt.logFile), []byte("x"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			got := listStepArtifacts(dir, tt.phaseIdx)
			found := false
			for _, f := range got {
				if f == tt.wantLog {
					found = true
					break
				}
			}
			if tt.wantLog != "" && !found {
				t.Errorf("listStepArtifacts(%d) missing %q; got %v", tt.phaseIdx, tt.wantLog, got)
			}
			if tt.wantLog == "" {
				for _, f := range got {
					if strings.HasPrefix(f, "logs/") {
						t.Errorf("listStepArtifacts(%d) unexpected logs entry %q", tt.phaseIdx, f)
					}
				}
			}
		})
	}
}
