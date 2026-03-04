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
	Status     string `json:"status"` // running, completed, failed, interrupted
}

func statePath(artifactsDir string) string {
	return filepath.Join(artifactsDir, "state.json")
}

// HasState reports whether a state file exists in the artifacts directory.
func HasState(artifactsDir string) bool {
	_, err := os.Stat(statePath(artifactsDir))
	return err == nil
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

// SetPhase sets the phase index for retry/from/loop jumps.
func (s *State) SetPhase(idx int) {
	s.PhaseIndex = idx
}

// TicketSummary holds the loaded state and cost data for one ticket.
type TicketSummary struct {
	Ticket       string
	ArtifactsDir string
	State        *State
	Costs        *CostData
}

// ListTickets reads all ticket subdirectories under baseArtifactsDir,
// loads each ticket's state and costs, and returns them sorted by directory name.
// Costs are loaded from baseAuditDir first, falling back to baseArtifactsDir.
// Directories that lack a state.json are skipped.
func ListTickets(baseArtifactsDir, baseAuditDir string) ([]TicketSummary, error) {
	entries, err := os.ReadDir(baseArtifactsDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var tickets []TicketSummary
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ad := filepath.Join(baseArtifactsDir, e.Name())

		// Skip directories without a state.json
		if _, err := os.Stat(statePath(ad)); err != nil {
			continue
		}

		st, err := Load(ad)
		if err != nil {
			continue
		}
		if st.Ticket == "" {
			st.Ticket = e.Name()
		}

		// Try audit dir first for costs, fall back to artifacts dir
		auditDir := filepath.Join(baseAuditDir, e.Name())
		costs, err := LoadCosts(auditDir)
		if err != nil {
			costs, err = LoadCosts(ad)
			if err != nil {
				costs = &CostData{}
			}
		}

		tickets = append(tickets, TicketSummary{
			Ticket:       st.Ticket,
			ArtifactsDir: ad,
			State:        st,
			Costs:        costs,
		})
	}
	return tickets, nil
}
