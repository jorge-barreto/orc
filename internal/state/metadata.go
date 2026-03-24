package state

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"time"
)

// PhaseMetadata holds structured metadata for a completed phase.
type PhaseMetadata struct {
	PhaseName    string    `json:"phase_name"`
	PhaseType    string    `json:"phase_type"`
	PhaseIndex   int       `json:"phase_index"`
	Model        string    `json:"model,omitempty"`
	Effort       string    `json:"effort,omitempty"`
	SessionID    string    `json:"session_id,omitempty"`
	StartTime    time.Time `json:"start_time"`
	EndTime      time.Time `json:"end_time"`
	DurationSecs float64   `json:"duration_seconds"`
	CostUSD      float64   `json:"cost_usd,omitempty"`
	InputTokens  int       `json:"input_tokens,omitempty"`
	OutputTokens int       `json:"output_tokens,omitempty"`
	ExitCode     int       `json:"exit_code"`
	ToolsUsed    []string  `json:"tools_used"`
	ToolsDenied  []string  `json:"tools_denied"`
	TimedOut     bool      `json:"timed_out,omitempty"`
}

// SaveMetadata writes phase metadata to a .meta.json file atomically.
// Nil slices are coerced to empty slices on a local copy so they serialize
// as [] not null; the caller's struct is not mutated.
func SaveMetadata(path string, meta *PhaseMetadata) error {
	local := *meta
	if local.ToolsUsed == nil {
		local.ToolsUsed = []string{}
	}
	if local.ToolsDenied == nil {
		local.ToolsDenied = []string{}
	}
	data, err := json.MarshalIndent(&local, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, data, 0644)
}

// LoadMetadata reads phase metadata from a .meta.json file.
// Returns nil, nil if the file does not exist.
func LoadMetadata(path string) (*PhaseMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var meta PhaseMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}
