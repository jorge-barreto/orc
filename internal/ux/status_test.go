package ux

import (
	"strings"
	"testing"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/state"
)

func TestRenderStatusAll_EmptyConfig(t *testing.T) {
	tickets := []state.TicketSummary{
		{Ticket: "PROJ-1", State: &state.State{PhaseIndex: 2, Status: "running"}, Costs: &state.CostData{}},
		{Ticket: "PROJ-2", State: &state.State{PhaseIndex: 4, Status: "completed"}, Costs: &state.CostData{}},
	}
	out := captureOutput(func() {
		RenderStatusAll(&config.Config{}, tickets)
	})
	if strings.Contains(out, "0/0") {
		t.Errorf("expected no '0/0' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "phase 3") {
		t.Errorf("expected 'phase 3', got:\n%s", out)
	}
	if !strings.Contains(out, "phase 5") {
		t.Errorf("expected 'phase 5', got:\n%s", out)
	}
}

func TestRenderStatusAll_WithPhases(t *testing.T) {
	cfg := &config.Config{
		Phases: []config.Phase{
			{Name: "plan"},
			{Name: "implement"},
			{Name: "test"},
			{Name: "review"},
			{Name: "ship"},
		},
	}
	tickets := []state.TicketSummary{
		{Ticket: "PROJ-1", State: &state.State{PhaseIndex: 1, Status: "running"}, Costs: &state.CostData{}},
		{Ticket: "PROJ-2", State: &state.State{PhaseIndex: 5, Status: "completed"}, Costs: &state.CostData{}},
	}
	out := captureOutput(func() {
		RenderStatusAll(cfg, tickets)
	})
	if !strings.Contains(out, "2/5 (implement)") {
		t.Errorf("expected '2/5 (implement)', got:\n%s", out)
	}
	if !strings.Contains(out, "5/5") {
		t.Errorf("expected '5/5', got:\n%s", out)
	}
}

func TestRenderStatusAll_WorkflowColumn(t *testing.T) {
	tickets := []state.TicketSummary{
		{Ticket: "PROJ-1", State: &state.State{PhaseIndex: 0, Status: "running", Workflow: "bugfix"}, Costs: &state.CostData{}},
	}
	out := captureOutput(func() {
		RenderStatusAll(&config.Config{}, tickets)
	})
	if !strings.Contains(out, "WORKFLOW") {
		t.Errorf("expected 'WORKFLOW' header, got:\n%s", out)
	}
	if !strings.Contains(out, "bugfix") {
		t.Errorf("expected 'bugfix' in output, got:\n%s", out)
	}
}

func TestRenderStatusAll_Empty(t *testing.T) {
	out := captureOutput(func() {
		RenderStatusAll(&config.Config{}, nil)
	})
	if !strings.Contains(out, "No tickets found.") {
		t.Errorf("expected 'No tickets found.', got:\n%s", out)
	}
}
