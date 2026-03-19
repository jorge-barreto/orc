package config

import (
	"strings"
	"testing"
)

func TestResolvePhaseRef_Number(t *testing.T) {
	phases := []Phase{
		{Name: "setup"},
		{Name: "implement"},
		{Name: "review"},
	}

	idx, err := ResolvePhaseRef("2", phases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx != 1 {
		t.Fatalf("got index %d, want 1", idx)
	}
}

func TestResolvePhaseRef_Name(t *testing.T) {
	phases := []Phase{
		{Name: "setup"},
		{Name: "implement"},
		{Name: "review"},
	}

	idx, err := ResolvePhaseRef("implement", phases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx != 1 {
		t.Fatalf("got index %d, want 1", idx)
	}
}

func TestResolvePhaseRef_FirstPhase(t *testing.T) {
	phases := []Phase{
		{Name: "setup"},
		{Name: "implement"},
	}

	// By number
	idx, err := ResolvePhaseRef("1", phases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx != 0 {
		t.Fatalf("got index %d, want 0", idx)
	}

	// By name
	idx, err = ResolvePhaseRef("setup", phases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx != 0 {
		t.Fatalf("got index %d, want 0", idx)
	}
}

func TestResolvePhaseRef_UnknownName(t *testing.T) {
	phases := []Phase{
		{Name: "setup"},
		{Name: "implement"},
		{Name: "review"},
	}

	_, err := ResolvePhaseRef("deploy", phases)
	if err == nil {
		t.Fatal("expected error for unknown name")
	}
	if !strings.Contains(err.Error(), "unknown phase") {
		t.Fatalf("expected 'unknown phase' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "setup, implement, review") {
		t.Fatalf("expected available phases listed in error, got: %v", err)
	}
}

func TestResolvePhaseRef_NumberOutOfRange(t *testing.T) {
	phases := []Phase{
		{Name: "setup"},
		{Name: "implement"},
	}

	_, err := ResolvePhaseRef("5", phases)
	if err == nil {
		t.Fatal("expected error for out-of-range number")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("expected 'out of range' in error, got: %v", err)
	}
}

func TestResolvePhaseRef_ZeroNumber(t *testing.T) {
	phases := []Phase{
		{Name: "setup"},
	}

	_, err := ResolvePhaseRef("0", phases)
	if err == nil {
		t.Fatal("expected error for zero")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("expected 'out of range' in error, got: %v", err)
	}
}

func TestResolvePhaseRef_NegativeNumber(t *testing.T) {
	phases := []Phase{
		{Name: "setup"},
	}

	_, err := ResolvePhaseRef("-1", phases)
	if err == nil {
		t.Fatal("expected error for negative number")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("expected 'out of range' in error, got: %v", err)
	}
}
