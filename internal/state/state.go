package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

const (
	StatusRunning     = "running"
	StatusCompleted   = "completed"
	StatusFailed      = "failed"
	StatusInterrupted = "interrupted"
)

const (
	FailCategoryLoopExhaustion = "loop_exhaustion"
	FailCategoryCostOverrun    = "cost_overrun"
	FailCategoryGateRejection  = "gate_rejection"
	FailCategoryScriptFailure  = "script_failure"
	FailCategoryOutputMissing  = "output_missing"
	FailCategoryInterrupted    = "interrupted"
	FailCategoryAgentError     = "agent_error"
)

type State struct {
	mu              sync.RWMutex
	PhaseIndex      int    `json:"phase_index"`
	Ticket          string `json:"ticket"`
	Workflow        string `json:"workflow,omitempty"`
	Status          string `json:"status"` // running, completed, failed, interrupted
	PhaseSessionID  string `json:"phase_session_id,omitempty"`
	FailureCategory string `json:"failure_category,omitempty"`
	FailureDetail   string `json:"failure_detail,omitempty"`
}

func statePath(artifactsDir string) string {
	return filepath.Join(artifactsDir, "state.json")
}

// HasState reports whether a state file exists in the artifacts directory.
func HasState(artifactsDir string) bool {
	_, err := os.Stat(statePath(artifactsDir))
	return err == nil
}

// ResolveStateDir finds the directory containing state.json for a ticket.
// It checks the live artifacts directory first, then falls back to the
// latest history entry. Returns an error if no state is found anywhere.
func ResolveStateDir(artifactsDir string) (string, error) {
	if HasState(artifactsDir) {
		return artifactsDir, nil
	}
	histDir, err := LatestHistoryDir(artifactsDir)
	if err != nil {
		return "", fmt.Errorf("checking history: %w", err)
	}
	if histDir == "" || !HasState(histDir) {
		return "", fmt.Errorf("no state found")
	}
	return histDir, nil
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
	s.mu.RLock()
	data, err := json.MarshalIndent(s, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	return WriteFileAtomic(statePath(artifactsDir), data, 0644)
}

// Advance increments the phase index.
func (s *State) Advance() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PhaseIndex++
	s.PhaseSessionID = ""
}

// SetPhase sets the phase index for retry/from/loop jumps.
func (s *State) SetPhase(idx int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PhaseIndex = idx
	s.PhaseSessionID = ""
}

// GetPhaseIndex returns the current phase index.
func (s *State) GetPhaseIndex() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.PhaseIndex
}

// GetTicket returns the ticket identifier.
func (s *State) GetTicket() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Ticket
}

// GetWorkflow returns the workflow name.
func (s *State) GetWorkflow() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Workflow
}

// GetStatus returns the current status.
func (s *State) GetStatus() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Status
}

// GetSessionID returns the phase session ID.
func (s *State) GetSessionID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.PhaseSessionID
}

// SetTicket sets the ticket identifier.
func (s *State) SetTicket(ticket string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Ticket = ticket
}

// SetWorkflow sets the workflow name.
func (s *State) SetWorkflow(workflow string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Workflow = workflow
}

// SetStatus sets the current status.
func (s *State) SetStatus(status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = status
}

// SetSessionID sets the phase session ID.
func (s *State) SetSessionID(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PhaseSessionID = id
}

// SetFailure sets the failure category and detail.
func (s *State) SetFailure(category, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.FailureCategory = category
	s.FailureDetail = detail
}

// GetFailureCategory returns the failure category.
func (s *State) GetFailureCategory() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.FailureCategory
}

// GetFailureDetail returns the failure detail.
func (s *State) GetFailureDetail() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.FailureDetail
}

// TicketSummary holds the loaded state and cost data for one ticket.
type TicketSummary struct {
	Ticket       string
	ArtifactsDir string
	State        *State
	Costs        *CostData
	Timing       *Timing
}

func loadTicketSummary(st *State, artifactsDir, auditDir string) TicketSummary {
	costs, err := LoadCosts(auditDir)
	if err != nil {
		costs, err = LoadCosts(artifactsDir)
		if err != nil {
			costs = &CostData{}
		}
	}
	timing, err := LoadTiming(auditDir)
	if err != nil {
		timing, _ = LoadTiming(artifactsDir)
	}
	return TicketSummary{
		Ticket:       st.GetTicket(),
		ArtifactsDir: artifactsDir,
		State:        st,
		Costs:        costs,
		Timing:       timing,
	}
}

// ListTickets reads all ticket subdirectories under baseArtifactsDir,
// loads each ticket's state and costs, and returns them sorted by directory name.
// Costs are loaded from baseAuditDir first, falling back to baseArtifactsDir.
// Directories that lack a state.json are skipped. Supports both flat layout
// (artifacts/<ticket>/state.json) and workflow-namespaced layout
// (artifacts/<workflow>/<ticket>/state.json).
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

		// Flat layout: artifacts/<ticket>/state.json
		if _, err := os.Stat(statePath(ad)); err == nil {
			st, err := Load(ad)
			if err != nil {
				continue
			}
			if st.GetTicket() == "" {
				st.SetTicket(e.Name())
			}
			auditDir := filepath.Join(baseAuditDir, e.Name())
			tickets = append(tickets, loadTicketSummary(st, ad, auditDir))
			continue
		}

		// Workflow-namespaced layout: artifacts/<workflow>/<ticket>/state.json
		subEntries, err := os.ReadDir(ad)
		if err != nil {
			continue
		}
		for _, se := range subEntries {
			if !se.IsDir() {
				continue
			}
			ticketDir := filepath.Join(ad, se.Name())
			if _, err := os.Stat(statePath(ticketDir)); err != nil {
				continue
			}
			st, err := Load(ticketDir)
			if err != nil {
				continue
			}
			if st.GetTicket() == "" {
				st.SetTicket(se.Name())
			}
			if st.GetWorkflow() == "" {
				st.SetWorkflow(e.Name())
			}
			auditDir := filepath.Join(baseAuditDir, e.Name(), se.Name())
			tickets = append(tickets, loadTicketSummary(st, ticketDir, auditDir))
		}
	}
	return tickets, nil
}
