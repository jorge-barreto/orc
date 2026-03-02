package state

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestListTickets_Empty(t *testing.T) {
	dir := t.TempDir()
	tickets, err := ListTickets(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 0 {
		t.Fatalf("expected 0 tickets, got %d", len(tickets))
	}
}

func TestListTickets_NoDir(t *testing.T) {
	tickets, err := ListTickets(filepath.Join(t.TempDir(), "nonexistent"))
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

	tickets, err := ListTickets(dir)
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

	tickets, err := ListTickets(dir)
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
