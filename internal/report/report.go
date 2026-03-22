package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/state"
)

// PhaseResult holds per-phase data for the report.
type PhaseResult struct {
	Number   int     `json:"number"`
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Duration string  `json:"duration"`
	Cost     string  `json:"cost"`
	CostUSD  float64 `json:"cost_usd"`
	Tokens   int     `json:"tokens"`
	Result   string  `json:"result"`
}

// LoopActivity records how many iterations a looping phase ran.
type LoopActivity struct {
	Phase      string `json:"phase"`
	Iterations int    `json:"iterations"`
}

// ArtifactFile describes a file produced by the run.
type ArtifactFile struct {
	Name string `json:"name"`
	Size string `json:"size"`
}

// ReportData is the top-level report structure.
type ReportData struct {
	SchemaVersion int            `json:"schema_version"`
	Ticket        string         `json:"ticket"`
	Workflow      string         `json:"workflow,omitempty"`
	Status        string         `json:"status"`
	Duration      string         `json:"duration"`
	Cost          string         `json:"cost"`
	TotalCostUSD  float64        `json:"total_cost_usd"`
	TotalTokens   int            `json:"total_tokens"`
	Phases        []PhaseResult  `json:"phases"`
	Loops         []LoopActivity `json:"loops"`
	Artifacts     []ArtifactFile `json:"artifacts"`
}

func lookupPhaseType(phases []config.Phase, name string) string {
	for _, p := range phases {
		if p.Name == name {
			return p.Type
		}
	}
	return "unknown"
}

// Build assembles a ReportData from artifacts on disk.
func Build(artifactsDir, auditDir string, st *state.State, phases []config.Phase) (*ReportData, error) {
	// Step 1: Load timing
	timing, err := state.LoadTiming(auditDir)
	if err != nil {
		timing, _ = state.LoadTiming(artifactsDir)
	}
	if timing == nil {
		timing = &state.Timing{}
	}

	// Step 2: Load costs
	costs, err := state.LoadCosts(auditDir)
	if err != nil {
		costs, err = state.LoadCosts(artifactsDir)
		if err != nil {
			costs = &state.CostData{}
		}
	}

	// Step 3: Load loop counts
	loopCounts, _ := state.LoadLoopCounts(artifactsDir)

	// Step 4: Build PhaseResult entries from timing entries
	phaseResults := []PhaseResult{}

	failedPhaseName := ""
	if st.GetStatus() == "failed" && st.GetPhaseIndex() < len(phases) {
		failedPhaseName = phases[st.GetPhaseIndex()].Name
	}

	// Build per-phase cost queues for name-based matching.
	// Handles parallel phases (costs in completion order, timing in config order)
	// and re-prompts (multiple cost entries for the same phase).
	costQueues := map[string][]int{}
	for idx, ce := range costs.Phases {
		costQueues[ce.Name] = append(costQueues[ce.Name], idx)
	}
	costConsumed := map[string]int{}

	for i, te := range timing.Entries {
		phaseType := lookupPhaseType(phases, te.Phase)

		var costUSD float64
		var tokens int
		costStr := "—"
		if indices, ok := costQueues[te.Phase]; ok {
			ci := costConsumed[te.Phase]
			if ci < len(indices) {
				ce := costs.Phases[indices[ci]]
				costConsumed[te.Phase] = ci + 1
				if ce.CostUSD > 0 {
					costStr = fmt.Sprintf("$%.2f", ce.CostUSD)
					costUSD = ce.CostUSD
				} else if ce.InputTokens+ce.OutputTokens > 0 {
					costStr = fmt.Sprintf("%s tokens", formatTokenCount(ce.InputTokens+ce.OutputTokens))
				}
				tokens = ce.InputTokens + ce.OutputTokens
			}
		}

		dur := te.Duration
		if dur == "" {
			dur = "—"
		}

		result := "Pass"
		if st.GetStatus() == "failed" && te.Phase == failedPhaseName {
			// Only mark the LAST timing entry for the failed phase as "Fail"
			isLast := true
			for j := i + 1; j < len(timing.Entries); j++ {
				if timing.Entries[j].Phase == failedPhaseName {
					isLast = false
					break
				}
			}
			if isLast {
				result = "Fail"
			}
		} else if te.End.IsZero() {
			result = "Interrupted"
			dur = "—"
			costStr = "—"
		} else if phaseType == "gate" {
			result = "Approved"
		}

		phaseResults = append(phaseResults, PhaseResult{
			Number:   i + 1,
			Name:     te.Phase,
			Type:     phaseType,
			Duration: dur,
			Cost:     costStr,
			CostUSD:  costUSD,
			Tokens:   tokens,
			Result:   result,
		})
	}

	// Fallback: if timing.Entries is empty and phase_index > 0, produce minimal entries
	if len(timing.Entries) == 0 && st.GetPhaseIndex() > 0 {
		limit := st.GetPhaseIndex()
		if limit > len(phases) {
			limit = len(phases)
		}
		for i := 0; i < limit; i++ {
			phaseResults = append(phaseResults, PhaseResult{
				Number:   i + 1,
				Name:     phases[i].Name,
				Type:     phases[i].Type,
				Duration: "—",
				Cost:     "—",
				Result:   "Pass",
			})
		}
	}

	// Step 5: Build LoopActivity — sort keys for deterministic output
	loops := []LoopActivity{}
	keys := make([]string, 0, len(loopCounts))
	for k := range loopCounts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if loopCounts[k] > 0 {
			loops = append(loops, LoopActivity{Phase: k, Iterations: loopCounts[k]})
		}
	}

	// Step 6: Build Artifacts — exclude subdirs and metadata files
	metadataFiles := map[string]bool{
		"state.json": true, "timing.json": true,
		"costs.json": true, "loop-counts.json": true,
	}
	entries, _ := os.ReadDir(artifactsDir)
	artifacts := []ArtifactFile{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if metadataFiles[e.Name()] {
			continue
		}
		info, _ := e.Info()
		var size string
		if info != nil {
			size = formatSize(info.Size())
		}
		artifacts = append(artifacts, ArtifactFile{Name: e.Name(), Size: size})
	}

	// Step 7: Compute totals and map status
	totalDur := "—"
	if d := timing.TotalElapsed(); d > 0 {
		totalDur = state.FormatDuration(d)
	}

	totalCost := "—"
	totalTokens := costs.TotalInputTokens + costs.TotalOutputTokens
	if costs.TotalCostUSD > 0 {
		totalCost = fmt.Sprintf("$%.2f (%s tokens)", costs.TotalCostUSD, formatTokenCount(totalTokens))
	} else if totalTokens > 0 {
		totalCost = fmt.Sprintf("%s tokens", formatTokenCount(totalTokens))
	}

	statusMap := map[string]string{
		"completed":   "Completed",
		"failed":      "Failed",
		"interrupted": "Interrupted",
		"running":     "Running",
	}
	displayStatus := statusMap[st.GetStatus()]
	if displayStatus == "" {
		displayStatus = st.GetStatus()
	}

	return &ReportData{
		SchemaVersion: 1,
		Ticket:        st.GetTicket(),
		Workflow:      st.GetWorkflow(),
		Status:        displayStatus,
		Duration:      totalDur,
		Cost:          totalCost,
		TotalCostUSD:  costs.TotalCostUSD,
		TotalTokens:   totalTokens,
		Phases:        phaseResults,
		Loops:         loops,
		Artifacts:     artifacts,
	}, nil
}

func formatSize(bytes int64) string {
	switch {
	case bytes < 1024:
		return fmt.Sprintf("%d bytes", bytes)
	case bytes < 1048576:
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(bytes)/1048576)
	}
}

func formatTokenCount(n int) string {
	s := fmt.Sprintf("%d", n)
	result := []byte{}
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

// RenderMarkdown writes a markdown-formatted report to w.
func RenderMarkdown(w io.Writer, r *ReportData) {
	fmt.Fprintf(w, "# Run Report: %s\n\n", r.Ticket)
	fmt.Fprintf(w, "**Status:** %s\n", r.Status)
	fmt.Fprintf(w, "**Duration:** %s\n", r.Duration)
	fmt.Fprintf(w, "**Cost:** %s\n", r.Cost)
	fmt.Fprintf(w, "\n## Phase Summary\n\n")
	fmt.Fprintf(w, "| # | Phase | Type | Duration | Cost | Result |\n")
	fmt.Fprintf(w, "|---|-------|------|----------|------|--------|\n")
	for _, p := range r.Phases {
		fmt.Fprintf(w, "| %d | %s | %s | %s | %s | %s |\n",
			p.Number, p.Name, p.Type, p.Duration, p.Cost, p.Result)
	}
	fmt.Fprintf(w, "\n## Loop Activity\n\n")
	if len(r.Loops) == 0 {
		fmt.Fprintf(w, "No retry loops triggered.\n")
	} else {
		fmt.Fprintf(w, "| Phase | Iterations |\n")
		fmt.Fprintf(w, "|-------|------------|\n")
		for _, l := range r.Loops {
			fmt.Fprintf(w, "| %s | %d |\n", l.Phase, l.Iterations)
		}
	}
	fmt.Fprintf(w, "\n## Artifacts\n\n")
	if len(r.Artifacts) == 0 {
		fmt.Fprintf(w, "No artifacts found.\n")
	} else {
		for _, a := range r.Artifacts {
			fmt.Fprintf(w, "- %s (%s)\n", a.Name, a.Size)
		}
	}
}

// RenderJSON writes a JSON-formatted report to w.
func RenderJSON(w io.Writer, r *ReportData) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}
