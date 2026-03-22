package stats

import (
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/jorge-barreto/orc/internal/state"
)

// RunData holds data collected from a single workflow run.
type RunData struct {
	Ticket          string
	Workflow        string
	Status          string // "completed", "failed", "interrupted"
	FailureCategory string
	CostUSD         float64
	Duration        time.Duration
	StartTime       time.Time
	PhaseCosts      map[string]float64       // phase name → total cost
	PhaseDurations  map[string]time.Duration // phase name → total duration
	PhaseIterations map[string]int           // phase name → dispatch count
}

// Stats holds aggregate statistics computed by Aggregate().
// Fields are populated by bead 9zj.6.2.
type Stats struct {
	TotalRuns    int
	TotalTickets int
	DateRange    string

	SuccessCount int
	SuccessRate  float64
	AvgCostUSD   float64
	P95CostUSD   float64
	AvgDuration  time.Duration
	P95Duration  time.Duration

	Phases   []PhaseStat
	Failures []FailureStat
	Weeks    []WeekStat
}

// PhaseStat holds per-phase aggregate statistics.
type PhaseStat struct {
	Name        string
	AvgCostUSD  float64
	AvgDuration time.Duration
	LoopRate    float64
	AvgIters    float64
	RunCount    int
}

// FailureStat holds per-category failure statistics.
type FailureStat struct {
	Category string
	Count    int
	Percent  float64
}

// WeekStat holds per-week aggregate statistics.
type WeekStat struct {
	WeekStart time.Time
	AvgCost   float64
	RunCount  int
}

// isAuditDir reports whether dir looks like an audit directory.
func isAuditDir(dir string) bool {
	for _, f := range []string{"timing.json", "costs.json", "state.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return true
		}
	}
	return false
}

// loadRunData loads a RunData from an audit directory.
// Returns (RunData, false) if the directory lacks state.json or cannot be loaded.
func loadRunData(dir string) (RunData, bool) {
	if !state.HasState(dir) {
		return RunData{}, false
	}
	st, err := state.Load(dir)
	if err != nil {
		return RunData{}, false
	}

	rd := RunData{
		Ticket:          st.GetTicket(),
		Workflow:        st.GetWorkflow(),
		Status:          st.GetStatus(),
		FailureCategory: st.GetFailureCategory(),
		PhaseCosts:      make(map[string]float64),
		PhaseDurations:  make(map[string]time.Duration),
		PhaseIterations: make(map[string]int),
	}

	// Timing: start time + per-phase durations + total duration
	timing, err := state.LoadTiming(dir)
	if err == nil && len(timing.Entries) > 0 {
		rd.StartTime = timing.Entries[0].Start
		rd.Duration = timing.TotalElapsed()
		for _, e := range timing.Entries {
			if !e.End.IsZero() {
				rd.PhaseDurations[e.Phase] += e.End.Sub(e.Start)
			}
		}
	}

	// Costs: total + per-phase
	costs, err := state.LoadCosts(dir)
	if err == nil {
		rd.CostUSD = costs.TotalCostUSD
		for _, p := range costs.Phases {
			rd.PhaseCosts[p.Name] += p.CostUSD
		}
	}

	// Dispatch counts: map phase index → count, resolved to phase names via costs
	attemptCounts, err := state.LoadAttemptCounts(dir)
	if err == nil {
		phaseIndexToName := make(map[int]string)
		if costs != nil {
			for _, p := range costs.Phases {
				if _, ok := phaseIndexToName[p.PhaseIndex]; !ok {
					phaseIndexToName[p.PhaseIndex] = p.Name
				}
			}
		}
		for idx, count := range attemptCounts {
			name, ok := phaseIndexToName[idx]
			if !ok {
				continue
			}
			if existing, e2 := rd.PhaseIterations[name]; !e2 || count > existing {
				rd.PhaseIterations[name] = count
			}
		}
	}

	return rd, true
}

// CollectRuns walks auditBaseDir and returns a RunData for each discovered audit run.
// Supports flat layout (audit/<ticket>/), workflow-namespaced layout
// (audit/<workflow>/<ticket>/), and rotated dirs (<ticket>-YYMMDD-HHMMSS/).
func CollectRuns(auditBaseDir string) ([]RunData, error) {
	entries, err := os.ReadDir(auditBaseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var runs []RunData
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(auditBaseDir, e.Name())

		// Flat layout (including rotated): audit/<ticket>/ or audit/<ticket>-YYMMDD-HHMMSS/
		if isAuditDir(dir) {
			if rd, ok := loadRunData(dir); ok {
				if rd.Ticket == "" {
					rd.Ticket = e.Name()
				}
				runs = append(runs, rd)
			}
			continue
		}

		// Workflow-namespaced layout: audit/<workflow>/<ticket>/
		subEntries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, se := range subEntries {
			if !se.IsDir() {
				continue
			}
			ticketDir := filepath.Join(dir, se.Name())
			if !isAuditDir(ticketDir) {
				continue
			}
			if rd, ok := loadRunData(ticketDir); ok {
				if rd.Ticket == "" {
					rd.Ticket = se.Name()
				}
				if rd.Workflow == "" {
					rd.Workflow = e.Name()
				}
				runs = append(runs, rd)
			}
		}
	}
	return runs, nil
}

// FilterRuns filters and sorts runs by ticket and/or count.
// If ticket is non-empty, only runs with that ticket are included.
// If last > 0, only the most recent `last` runs are returned.
// Results are returned in chronological (ascending StartTime) order.
func FilterRuns(runs []RunData, ticket string, last int) []RunData {
	filtered := runs
	if ticket != "" {
		var out []RunData
		for _, r := range runs {
			if r.Ticket == ticket {
				out = append(out, r)
			}
		}
		filtered = out
	}

	// Sort descending by StartTime (newest first)
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].StartTime.After(filtered[j].StartTime)
	})

	// Take first `last` entries
	if last > 0 && len(filtered) > last {
		filtered = filtered[:last]
	}

	// Re-reverse to chronological order
	for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
		filtered[i], filtered[j] = filtered[j], filtered[i]
	}

	return filtered
}
