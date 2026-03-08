package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasState(t *testing.T) {
	dir := t.TempDir()
	if HasState(dir) {
		t.Fatal("HasState should return false for empty dir")
	}

	// Write a state file, then check again.
	st := &State{Status: StatusRunning}
	if err := st.Save(dir); err != nil {
		t.Fatal(err)
	}
	if !HasState(dir) {
		t.Fatal("HasState should return true after Save")
	}
}

func TestLoad_NoExistingState(t *testing.T) {
	dir := t.TempDir()
	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.PhaseIndex != 0 {
		t.Fatalf("PhaseIndex = %d, want 0", st.PhaseIndex)
	}
	if st.Status != StatusRunning {
		t.Fatalf("Status = %q, want running", st.Status)
	}
}

func TestSaveAndLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	original := &State{
		PhaseIndex: 3,
		Ticket:     "ABC-123",
		Status:     StatusCompleted,
	}
	if err := original.Save(dir); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PhaseIndex != 3 {
		t.Fatalf("PhaseIndex = %d, want 3", loaded.PhaseIndex)
	}
	if loaded.Ticket != "ABC-123" {
		t.Fatalf("Ticket = %q", loaded.Ticket)
	}
	if loaded.Status != StatusCompleted {
		t.Fatalf("Status = %q", loaded.Status)
	}
}

func TestAdvance(t *testing.T) {
	s := &State{PhaseIndex: 2}
	s.Advance()
	if s.PhaseIndex != 3 {
		t.Fatalf("PhaseIndex = %d, want 3", s.PhaseIndex)
	}
}

func TestSetPhase(t *testing.T) {
	s := &State{PhaseIndex: 5}
	s.SetPhase(1)
	if s.PhaseIndex != 1 {
		t.Fatalf("PhaseIndex = %d, want 1", s.PhaseIndex)
	}
}

func TestSaveAndLoad_RoundTrip_WithSessionID(t *testing.T) {
	dir := t.TempDir()
	original := &State{
		PhaseIndex:     2,
		Ticket:         "ABC-123",
		Status:         StatusInterrupted,
		PhaseSessionID: "session-uuid-1234",
	}
	if err := original.Save(dir); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PhaseSessionID != "session-uuid-1234" {
		t.Fatalf("PhaseSessionID = %q, want %q", loaded.PhaseSessionID, "session-uuid-1234")
	}
}

func TestAdvance_ClearsSessionID(t *testing.T) {
	s := &State{PhaseIndex: 2, PhaseSessionID: "session-123"}
	s.Advance()
	if s.PhaseSessionID != "" {
		t.Fatalf("PhaseSessionID = %q after Advance, want empty", s.PhaseSessionID)
	}
	if s.PhaseIndex != 3 {
		t.Fatalf("PhaseIndex = %d, want 3", s.PhaseIndex)
	}
}

func TestSetPhase_ClearsSessionID(t *testing.T) {
	s := &State{PhaseIndex: 5, PhaseSessionID: "session-456"}
	s.SetPhase(1)
	if s.PhaseSessionID != "" {
		t.Fatalf("PhaseSessionID = %q after SetPhase, want empty", s.PhaseSessionID)
	}
	if s.PhaseIndex != 1 {
		t.Fatalf("PhaseIndex = %d, want 1", s.PhaseIndex)
	}
}

func TestLoad_BackwardsCompatible_NoSessionID(t *testing.T) {
	dir := t.TempDir()
	// Write a state.json without phase_session_id (old format)
	data := []byte(`{"phase_index":1,"ticket":"T-1","status":"interrupted"}`)
	if err := os.WriteFile(filepath.Join(dir, "state.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.PhaseSessionID != "" {
		t.Fatalf("PhaseSessionID = %q, want empty for old format", st.PhaseSessionID)
	}
}

func TestListTickets_Empty(t *testing.T) {
	dir := t.TempDir()
	tickets, err := ListTickets(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 0 {
		t.Fatalf("expected 0 tickets, got %d", len(tickets))
	}
}

func TestListTickets_NoDir(t *testing.T) {
	tickets, err := ListTickets(filepath.Join(t.TempDir(), "nonexistent"), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 0 {
		t.Fatalf("expected 0 tickets, got %d", len(tickets))
	}
}

func TestListTickets_MultipleTickets(t *testing.T) {
	dir := t.TempDir()

	for _, ticket := range []string{"T-001", "T-002"} {
		ad := filepath.Join(dir, ticket)
		os.MkdirAll(ad, 0755)
		st := &State{PhaseIndex: 2, Ticket: ticket, Status: StatusCompleted}
		st.Save(ad)
	}

	tickets, err := ListTickets(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 2 {
		t.Fatalf("expected 2 tickets, got %d", len(tickets))
	}
	if tickets[0].Ticket != "T-001" {
		t.Fatalf("tickets[0].Ticket = %q, want T-001", tickets[0].Ticket)
	}
	if tickets[1].Ticket != "T-002" {
		t.Fatalf("tickets[1].Ticket = %q, want T-002", tickets[1].Ticket)
	}
}

func TestListTickets_SkipDirWithoutState(t *testing.T) {
	dir := t.TempDir()

	ad := filepath.Join(dir, "T-001")
	os.MkdirAll(ad, 0755)
	st := &State{PhaseIndex: 1, Ticket: "T-001", Status: StatusRunning}
	st.Save(ad)

	os.MkdirAll(filepath.Join(dir, "empty-dir"), 0755)

	tickets, err := ListTickets(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 {
		t.Fatalf("expected 1 ticket, got %d", len(tickets))
	}
	if tickets[0].Ticket != "T-001" {
		t.Fatalf("tickets[0].Ticket = %q, want T-001", tickets[0].Ticket)
	}
}
