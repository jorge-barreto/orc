package state

import (
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
	if st.Status != "running" {
		t.Fatalf("Status = %q, want running", st.Status)
	}
}

func TestSaveAndLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	original := &State{
		PhaseIndex: 3,
		Ticket:     "ABC-123",
		Status:     "completed",
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
	if loaded.Status != "completed" {
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
