package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndLoadMetadata(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0755); err != nil {
		t.Fatal(err)
	}
	path := MetaPath(dir, 0)
	now := time.Now().Truncate(time.Second)
	meta := &PhaseMetadata{
		PhaseName:    "build",
		PhaseType:    "agent",
		PhaseIndex:   0,
		Model:        "sonnet",
		Effort:       "low",
		SessionID:    "sess-abc",
		StartTime:    now,
		EndTime:      now.Add(90 * time.Second),
		DurationSecs: 90.0,
		CostUSD:      0.123,
		InputTokens:  1000,
		OutputTokens: 500,
		ExitCode:     0,
		ToolsUsed:    []string{"Read", "Edit"},
		ToolsDenied:  []string{"Bash"},
		TimedOut:     false,
	}
	if err := SaveMetadata(path, meta); err != nil {
		t.Fatal(err)
	}
	got, err := LoadMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected non-nil metadata")
	}
	if got.PhaseName != "build" {
		t.Errorf("PhaseName = %q, want %q", got.PhaseName, "build")
	}
	if got.PhaseType != "agent" {
		t.Errorf("PhaseType = %q, want %q", got.PhaseType, "agent")
	}
	if got.Model != "sonnet" {
		t.Errorf("Model = %q, want %q", got.Model, "sonnet")
	}
	if got.SessionID != "sess-abc" {
		t.Errorf("SessionID = %q, want %q", got.SessionID, "sess-abc")
	}
	if got.CostUSD != 0.123 {
		t.Errorf("CostUSD = %v, want 0.123", got.CostUSD)
	}
	if got.InputTokens != 1000 || got.OutputTokens != 500 {
		t.Errorf("tokens = %d/%d, want 1000/500", got.InputTokens, got.OutputTokens)
	}
	if len(got.ToolsUsed) != 2 || got.ToolsUsed[0] != "Read" || got.ToolsUsed[1] != "Edit" {
		t.Errorf("ToolsUsed = %v, want [Read Edit]", got.ToolsUsed)
	}
	if len(got.ToolsDenied) != 1 || got.ToolsDenied[0] != "Bash" {
		t.Errorf("ToolsDenied = %v, want [Bash]", got.ToolsDenied)
	}
}

func TestLoadMetadata_NotExist(t *testing.T) {
	dir := t.TempDir()
	meta, err := LoadMetadata(filepath.Join(dir, "phase-1.meta.json"))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if meta != nil {
		t.Fatalf("expected nil metadata for missing file, got %+v", meta)
	}
}

func TestSaveMetadata_NilSlices(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0755); err != nil {
		t.Fatal(err)
	}
	path := MetaPath(dir, 0)
	meta := &PhaseMetadata{
		PhaseName:   "build",
		PhaseType:   "script",
		ToolsUsed:   nil,
		ToolsDenied: nil,
	}
	if err := SaveMetadata(path, meta); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"tools_used", "tools_denied"} {
		v, ok := raw[field]
		if !ok {
			t.Errorf("field %q missing from JSON", field)
			continue
		}
		arr, ok := v.([]any)
		if !ok {
			t.Errorf("field %q is not an array, got %T", field, v)
			continue
		}
		if len(arr) != 0 {
			t.Errorf("field %q should be [], got %v", field, arr)
		}
	}
}

func TestSaveMetadata_DoesNotMutateInput(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0755); err != nil {
		t.Fatal(err)
	}
	path := MetaPath(dir, 0)
	meta := &PhaseMetadata{
		PhaseName:   "build",
		PhaseType:   "script",
		ToolsUsed:   nil,
		ToolsDenied: nil,
	}
	if err := SaveMetadata(path, meta); err != nil {
		t.Fatal(err)
	}
	if meta.ToolsUsed != nil {
		t.Errorf("ToolsUsed was mutated: got %v, want nil", meta.ToolsUsed)
	}
	if meta.ToolsDenied != nil {
		t.Errorf("ToolsDenied was mutated: got %v, want nil", meta.ToolsDenied)
	}
}
