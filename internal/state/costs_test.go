package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCosts_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	costs := &CostData{}
	costs.Record("plan", 0, 0.5, 15000, 8000, 3000, 12000, 1)
	costs.Record("implement", 2, 0.25, 45000, 22000, 5000, 40000, 3)

	if err := costs.Flush(dir); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadCosts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Phases) != 2 {
		t.Fatalf("got %d phases, want 2", len(loaded.Phases))
	}
	if loaded.Phases[0].Name != "plan" {
		t.Fatalf("phase[0].Name = %q", loaded.Phases[0].Name)
	}
	if loaded.Phases[0].CostUSD != 0.5 {
		t.Fatalf("phase[0].CostUSD = %f", loaded.Phases[0].CostUSD)
	}
	if loaded.Phases[0].InputTokens != 15000 {
		t.Fatalf("phase[0].InputTokens = %d", loaded.Phases[0].InputTokens)
	}
	if loaded.Phases[1].Turns != 3 {
		t.Fatalf("phase[1].Turns = %d", loaded.Phases[1].Turns)
	}
	if loaded.TotalCostUSD != 0.75 {
		t.Fatalf("TotalCostUSD = %f, want 0.75", loaded.TotalCostUSD)
	}
	if loaded.TotalInputTokens != 60000 {
		t.Fatalf("TotalInputTokens = %d, want 60000", loaded.TotalInputTokens)
	}
	if loaded.TotalOutputTokens != 30000 {
		t.Fatalf("TotalOutputTokens = %d, want 30000", loaded.TotalOutputTokens)
	}
	if loaded.TotalCacheCreationInputTokens != 8000 {
		t.Fatalf("TotalCacheCreationInputTokens = %d, want 8000", loaded.TotalCacheCreationInputTokens)
	}
	if loaded.TotalCacheReadInputTokens != 52000 {
		t.Fatalf("TotalCacheReadInputTokens = %d, want 52000", loaded.TotalCacheReadInputTokens)
	}
	if loaded.Phases[0].CacheCreationInputTokens != 3000 {
		t.Fatalf("phase[0].CacheCreationInputTokens = %d", loaded.Phases[0].CacheCreationInputTokens)
	}
	if loaded.Phases[0].CacheReadInputTokens != 12000 {
		t.Fatalf("phase[0].CacheReadInputTokens = %d", loaded.Phases[0].CacheReadInputTokens)
	}
}

func TestCosts_NoFile(t *testing.T) {
	dir := t.TempDir()
	costs, err := LoadCosts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(costs.Phases) != 0 {
		t.Fatalf("expected empty phases, got %d", len(costs.Phases))
	}
	if costs.TotalCostUSD != 0 {
		t.Fatalf("TotalCostUSD = %f, want 0", costs.TotalCostUSD)
	}
}

func TestCosts_ZeroCostSubscriptionMode(t *testing.T) {
	dir := t.TempDir()
	costs := &CostData{}
	costs.Record("plan", 0, 0, 15000, 8000, 0, 0, 1)

	if err := costs.Flush(dir); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadCosts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Phases[0].CostUSD != 0 {
		t.Fatalf("CostUSD = %f, want 0", loaded.Phases[0].CostUSD)
	}
	if loaded.Phases[0].InputTokens != 15000 {
		t.Fatalf("InputTokens = %d, want 15000", loaded.Phases[0].InputTokens)
	}
	if loaded.TotalCostUSD != 0 {
		t.Fatalf("TotalCostUSD = %f, want 0", loaded.TotalCostUSD)
	}
}

func TestCostData_TotalCost(t *testing.T) {
	c := &CostData{}
	c.Record("a", 0, 0.5, 100, 50, 0, 0, 1)
	c.Record("b", 1, 0.25, 200, 100, 0, 0, 1)
	if got := c.TotalCost(); got != 0.75 {
		t.Fatalf("TotalCost() = %f, want 0.75", got)
	}
}

func TestCostData_PhaseCost(t *testing.T) {
	c := &CostData{}
	c.Record("a", 0, 0.5, 100, 50, 0, 0, 1)
	c.Record("b", 1, 0.3, 200, 100, 0, 0, 1)
	c.Record("a", 0, 0.2, 100, 50, 0, 0, 1)
	if got := c.PhaseCost("a"); got != 0.7 {
		t.Fatalf("PhaseCost(\"a\") = %f, want 0.7", got)
	}
	if got := c.PhaseCost("b"); got != 0.3 {
		t.Fatalf("PhaseCost(\"b\") = %f, want 0.3", got)
	}
	if got := c.PhaseCost("c"); got != 0 {
		t.Fatalf("PhaseCost(\"c\") = %f, want 0", got)
	}
}

func TestCostData_TotalCostEmpty(t *testing.T) {
	c := &CostData{}
	if got := c.TotalCost(); got != 0 {
		t.Fatalf("TotalCost() = %f, want 0", got)
	}
}

func TestCostData_PhaseCostEmpty(t *testing.T) {
	c := &CostData{}
	if got := c.PhaseCost("any"); got != 0 {
		t.Fatalf("PhaseCost(\"any\") = %f, want 0", got)
	}
}

func TestCosts_JSONFormat(t *testing.T) {
	dir := t.TempDir()
	costs := &CostData{}
	costs.Record("plan", 0, 0.5, 15000, 8000, 3000, 12000, 1)

	if err := costs.Flush(dir); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "costs.json"))
	if err != nil {
		t.Fatal(err)
	}

	// Verify it's valid JSON with expected structure
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := raw["phases"]; !ok {
		t.Fatal("missing 'phases' key")
	}
	if _, ok := raw["total_cost_usd"]; !ok {
		t.Fatal("missing 'total_cost_usd' key")
	}
	if _, ok := raw["total_input_tokens"]; !ok {
		t.Fatal("missing 'total_input_tokens' key")
	}
	if _, ok := raw["total_output_tokens"]; !ok {
		t.Fatal("missing 'total_output_tokens' key")
	}
}
