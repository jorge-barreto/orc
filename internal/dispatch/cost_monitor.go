package dispatch

import (
	"strings"
)

// modelRates holds per-token prices in USD for a Claude model.
type modelRates struct {
	inputPerToken         float64
	outputPerToken        float64
	cacheReadPerToken     float64
	cacheCreationPerToken float64
}

// lookupRates returns the per-token pricing for a model string. The model
// can be a short alias (haiku, sonnet, opus) or a full ID like
// "claude-haiku-4-5-20251001". Unknown models return sonnet rates as a
// conservative default — overestimates kill faster, under is a safety bug.
func lookupRates(model string) modelRates {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "haiku"):
		// Haiku 4.5: $1/Mtok input, $5/Mtok output, $0.10/Mtok cache read,
		// $1.25/Mtok cache creation.
		return modelRates{
			inputPerToken:         1.0 / 1_000_000,
			outputPerToken:        5.0 / 1_000_000,
			cacheReadPerToken:     0.10 / 1_000_000,
			cacheCreationPerToken: 1.25 / 1_000_000,
		}
	case strings.Contains(m, "opus"):
		// Opus 4: $15/Mtok input, $75/Mtok output.
		return modelRates{
			inputPerToken:         15.0 / 1_000_000,
			outputPerToken:        75.0 / 1_000_000,
			cacheReadPerToken:     1.5 / 1_000_000,
			cacheCreationPerToken: 18.75 / 1_000_000,
		}
	default:
		// Sonnet 4 and unknown: $3/Mtok input, $15/Mtok output.
		return modelRates{
			inputPerToken:         3.0 / 1_000_000,
			outputPerToken:        15.0 / 1_000_000,
			cacheReadPerToken:     0.30 / 1_000_000,
			cacheCreationPerToken: 3.75 / 1_000_000,
		}
	}
}

// costMonitor tracks running cost estimate during a claude stream and
// reports when the cap is exceeded. Zero value is a disabled monitor
// (maxCost <= 0 means no enforcement).
type costMonitor struct {
	maxCost float64
	rates   modelRates

	// Set from message_start usage block.
	inputTokens              int
	cacheCreationInputTokens int
	cacheReadInputTokens     int

	// Accumulated during content_block_delta events.
	outputCharsEstimate int

	// Once tripped, stays tripped.
	exceeded bool
}

// newCostMonitor creates a monitor bound to phase.MaxCost. If maxCost <= 0,
// the returned monitor short-circuits (no enforcement).
func newCostMonitor(maxCost float64, model string) *costMonitor {
	return &costMonitor{
		maxCost: maxCost,
		rates:   lookupRates(model),
	}
}

// setBaseline records token counts from a message_start usage block. This
// is the first point at which input + cache costs are known.
func (m *costMonitor) setBaseline(inputTokens, cacheCreation, cacheRead int) {
	if m == nil || m.maxCost <= 0 {
		return
	}
	m.inputTokens = inputTokens
	m.cacheCreationInputTokens = cacheCreation
	m.cacheReadInputTokens = cacheRead
}

// addOutputText accumulates output-text bytes as they stream. Output token
// count is approximated as len(text)/4 (a standard rough tokenizer ratio).
func (m *costMonitor) addOutputText(text string) {
	if m == nil || m.maxCost <= 0 {
		return
	}
	m.outputCharsEstimate += len(text)
}

// estimatedCost returns the current live cost estimate.
func (m *costMonitor) estimatedCost() float64 {
	if m == nil {
		return 0
	}
	outputTokens := m.outputCharsEstimate / 4
	return float64(m.inputTokens)*m.rates.inputPerToken +
		float64(outputTokens)*m.rates.outputPerToken +
		float64(m.cacheReadInputTokens)*m.rates.cacheReadPerToken +
		float64(m.cacheCreationInputTokens)*m.rates.cacheCreationPerToken
}

// overBudget reports whether the current estimate exceeds the cap.
// Latched: once true, stays true.
func (m *costMonitor) overBudget() bool {
	if m == nil || m.maxCost <= 0 {
		return false
	}
	if m.exceeded {
		return true
	}
	if m.estimatedCost() > m.maxCost {
		m.exceeded = true
	}
	return m.exceeded
}
