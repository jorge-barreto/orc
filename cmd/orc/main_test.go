package main

import (
	"strings"
	"testing"

	"github.com/jorge-barreto/orc/internal/config"
)

func TestResolvePhaseRef_Number(t *testing.T) {
	phases := []config.Phase{
		{Name: "setup"},
		{Name: "implement"},
		{Name: "review"},
	}

	idx, err := resolvePhaseRef("2", phases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx != 1 {
		t.Fatalf("got index %d, want 1", idx)
	}
}

func TestResolvePhaseRef_Name(t *testing.T) {
	phases := []config.Phase{
		{Name: "setup"},
		{Name: "implement"},
		{Name: "review"},
	}

	idx, err := resolvePhaseRef("implement", phases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx != 1 {
		t.Fatalf("got index %d, want 1", idx)
	}
}

func TestResolvePhaseRef_FirstPhase(t *testing.T) {
	phases := []config.Phase{
		{Name: "setup"},
		{Name: "implement"},
	}

	// By number
	idx, err := resolvePhaseRef("1", phases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx != 0 {
		t.Fatalf("got index %d, want 0", idx)
	}

	// By name
	idx, err = resolvePhaseRef("setup", phases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx != 0 {
		t.Fatalf("got index %d, want 0", idx)
	}
}

func TestResolvePhaseRef_UnknownName(t *testing.T) {
	phases := []config.Phase{
		{Name: "setup"},
		{Name: "implement"},
		{Name: "review"},
	}

	_, err := resolvePhaseRef("deploy", phases)
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
	phases := []config.Phase{
		{Name: "setup"},
		{Name: "implement"},
	}

	_, err := resolvePhaseRef("5", phases)
	if err == nil {
		t.Fatal("expected error for out-of-range number")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("expected 'out of range' in error, got: %v", err)
	}
}

func TestResolvePhaseRef_ZeroNumber(t *testing.T) {
	phases := []config.Phase{
		{Name: "setup"},
	}

	_, err := resolvePhaseRef("0", phases)
	if err == nil {
		t.Fatal("expected error for zero")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("expected 'out of range' in error, got: %v", err)
	}
}

func TestResolvePhaseRef_NegativeNumber(t *testing.T) {
	phases := []config.Phase{
		{Name: "setup"},
	}

	_, err := resolvePhaseRef("-1", phases)
	if err == nil {
		t.Fatal("expected error for negative number")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("expected 'out of range' in error, got: %v", err)
	}
}
