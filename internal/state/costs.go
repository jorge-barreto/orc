package state

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// CostEntry holds cost and token data for a single phase.
type CostEntry struct {
	Name                     string  `json:"name"`
	PhaseIndex               int     `json:"phase_index"`
	CostUSD                  float64 `json:"cost_usd"`
	InputTokens              int     `json:"input_tokens"`
	OutputTokens             int     `json:"output_tokens"`
	CacheCreationInputTokens int     `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int     `json:"cache_read_input_tokens"`
	Turns                    int     `json:"turns"`
}

// CostData holds aggregate cost data for the entire run.
type CostData struct {
	mu                            sync.Mutex
	Phases                        []CostEntry `json:"phases"`
	TotalCostUSD                  float64     `json:"total_cost_usd"`
	TotalInputTokens              int         `json:"total_input_tokens"`
	TotalOutputTokens             int         `json:"total_output_tokens"`
	TotalCacheCreationInputTokens int         `json:"total_cache_creation_input_tokens"`
	TotalCacheReadInputTokens     int         `json:"total_cache_read_input_tokens"`
}

func costsPath(artifactsDir string) string {
	return filepath.Join(artifactsDir, "costs.json")
}

// LoadCosts reads cost data from the artifacts directory.
// Returns empty CostData if the file does not exist.
func LoadCosts(artifactsDir string) (*CostData, error) {
	path := costsPath(artifactsDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &CostData{}, nil
		}
		return nil, err
	}
	var c CostData
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Record appends a cost entry and updates totals.
func (c *CostData) Record(name string, phaseIndex int, costUSD float64, inputTokens, outputTokens, cacheCreationInputTokens, cacheReadInputTokens, turns int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Phases = append(c.Phases, CostEntry{
		Name:                     name,
		PhaseIndex:               phaseIndex,
		CostUSD:                  costUSD,
		InputTokens:              inputTokens,
		OutputTokens:             outputTokens,
		CacheCreationInputTokens: cacheCreationInputTokens,
		CacheReadInputTokens:     cacheReadInputTokens,
		Turns:                    turns,
	})
	c.TotalCostUSD += costUSD
	c.TotalInputTokens += inputTokens
	c.TotalOutputTokens += outputTokens
	c.TotalCacheCreationInputTokens += cacheCreationInputTokens
	c.TotalCacheReadInputTokens += cacheReadInputTokens
}

// TotalCost returns the cumulative cost across all phases (thread-safe).
func (c *CostData) TotalCost() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.TotalCostUSD
}

// PhaseCost returns the cumulative cost for a specific phase name (thread-safe).
// If the phase was invoked multiple times (e.g., via retry loops), costs are summed.
func (c *CostData) PhaseCost(name string) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	var total float64
	for _, e := range c.Phases {
		if e.Name == name {
			total += e.CostUSD
		}
	}
	return total
}

// Merge folds another CostData into this one (e.g., child workflow costs into parent).
// Entry names are prefixed with the given prefix for disambiguation.
func (c *CostData) Merge(other *CostData, prefix string) {
	if other == nil {
		return
	}
	other.mu.Lock()
	entries := make([]CostEntry, len(other.Phases))
	copy(entries, other.Phases)
	totalCost := other.TotalCostUSD
	totalIn := other.TotalInputTokens
	totalOut := other.TotalOutputTokens
	totalCacheCreate := other.TotalCacheCreationInputTokens
	totalCacheRead := other.TotalCacheReadInputTokens
	other.mu.Unlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range entries {
		if prefix != "" {
			e.Name = prefix + "/" + e.Name
		}
		c.Phases = append(c.Phases, e)
	}
	c.TotalCostUSD += totalCost
	c.TotalInputTokens += totalIn
	c.TotalOutputTokens += totalOut
	c.TotalCacheCreationInputTokens += totalCacheCreate
	c.TotalCacheReadInputTokens += totalCacheRead
}

// Flush writes cost data to disk atomically.
func (c *CostData) Flush(artifactsDir string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(costsPath(artifactsDir), data, 0644)
}
