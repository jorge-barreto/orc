package state

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	StatusRunning     = "running"
	StatusCompleted   = "completed"
	StatusFailed      = "failed"
	StatusInterrupted = "interrupted"
)

type State struct {
	PhaseIndex int    `json:"phase_index"`
	Ticket     string `json:"ticket"`
	Status     string `json:"status"` // running, completed, failed
}

func statePath(artifactsDir string) string {
	return filepath.Join(artifactsDir, "state.json")
}

// Load reads the state from the artifacts directory. Returns a new state if not found.
func Load(artifactsDir string) (*State, error) {
	path := statePath(artifactsDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &State{Status: StatusRunning}, nil
		}
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Save writes the state to the artifacts directory.
func (s *State) Save(artifactsDir string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(statePath(artifactsDir), data, 0644)
}

// Advance increments the phase index.
func (s *State) Advance() {
	s.PhaseIndex++
}

// SetPhase sets the phase index for retry/from/on-fail jumps.
func (s *State) SetPhase(idx int) {
	s.PhaseIndex = idx
}
