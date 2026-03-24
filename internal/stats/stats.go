package stats

import (
	"math"
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
	Category string  `json:"category"`
	Count    int     `json:"count"`
	Percent  float64 `json:"percent"`
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
	if err == nil {
		te := timing.Entries()
		if len(te) > 0 {
			rd.StartTime = te[0].Start
			rd.Duration = timing.TotalElapsed()
			for _, e := range te {
				if !e.End.IsZero() {
					rd.PhaseDurations[e.Phase] += e.End.Sub(e.Start)
				}
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

// Aggregate computes aggregate statistics from a slice of RunData.
// Only terminal runs (completed, failed, interrupted) are counted.
func Aggregate(runs []RunData) *Stats {
	// 1. Filter to terminal statuses only
	var filtered []RunData
	for _, r := range runs {
		if r.Status == "completed" || r.Status == "failed" || r.Status == "interrupted" {
			filtered = append(filtered, r)
		}
	}

	s := &Stats{TotalRuns: len(filtered)}
	if len(filtered) == 0 {
		return s
	}

	// 2. TotalTickets = unique ticket count
	tickets := make(map[string]struct{})
	for _, r := range filtered {
		tickets[r.Ticket] = struct{}{}
	}
	s.TotalTickets = len(tickets)

	// 3. DateRange from earliest/latest StartTime
	var earliest, latest time.Time
	for _, r := range filtered {
		if r.StartTime.IsZero() {
			continue
		}
		if earliest.IsZero() || r.StartTime.Before(earliest) {
			earliest = r.StartTime
		}
		if r.StartTime.After(latest) {
			latest = r.StartTime
		}
	}
	if !earliest.IsZero() {
		if earliest.Year() == latest.Year() && earliest.YearDay() == latest.YearDay() {
			s.DateRange = earliest.Format("Jan 2, 2006")
		} else {
			s.DateRange = earliest.Format("Jan 2") + " – " + latest.Format("Jan 2, 2006")
		}
	}

	// 4. SuccessCount / SuccessRate
	for _, r := range filtered {
		if r.Status == "completed" {
			s.SuccessCount++
		}
	}
	s.SuccessRate = float64(s.SuccessCount) / float64(len(filtered)) * 100

	// 5. Avg cost / P95 cost (exclude zero-cost runs from avg)
	var costs []float64
	for _, r := range filtered {
		if r.CostUSD > 0 {
			costs = append(costs, r.CostUSD)
		}
	}
	if len(costs) > 0 {
		var sum float64
		for _, c := range costs {
			sum += c
		}
		s.AvgCostUSD = sum / float64(len(costs))
		if len(costs) >= 2 {
			sorted := make([]float64, len(costs))
			copy(sorted, costs)
			sort.Float64s(sorted)
			idx := int(math.Ceil(0.95*float64(len(sorted)))) - 1
			s.P95CostUSD = sorted[idx]
		} else {
			s.P95CostUSD = costs[0]
		}
	}

	// 6. Avg duration / P95 duration (exclude zero-duration runs)
	var durations []time.Duration
	for _, r := range filtered {
		if r.Duration > 0 {
			durations = append(durations, r.Duration)
		}
	}
	if len(durations) > 0 {
		var sum time.Duration
		for _, d := range durations {
			sum += d
		}
		s.AvgDuration = sum / time.Duration(len(durations))
		if len(durations) >= 2 {
			sorted := make([]time.Duration, len(durations))
			copy(sorted, durations)
			sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
			idx := int(math.Ceil(0.95*float64(len(sorted)))) - 1
			s.P95Duration = sorted[idx]
		} else {
			s.P95Duration = durations[0]
		}
	}

	// 7. Phase breakdown
	// Collect all phase names in order of first appearance
	type phaseAccum struct {
		costSum   float64
		costCount int
		durSum    time.Duration
		durCount  int
		loopCount int // runs where iterations > 1
		iterSum   float64
		iterCount int // runs where iterations > 1
		runCount  int
		firstSeen int // order for sorting
	}
	phaseMap := make(map[string]*phaseAccum)
	phaseOrder := []string{}
	for _, r := range filtered {
		// Collect all phase names seen in this run
		phasesSeen := make(map[string]struct{})
		for name := range r.PhaseCosts {
			phasesSeen[name] = struct{}{}
		}
		for name := range r.PhaseDurations {
			phasesSeen[name] = struct{}{}
		}
		for name := range r.PhaseIterations {
			phasesSeen[name] = struct{}{}
		}
		for name := range phasesSeen {
			if _, ok := phaseMap[name]; !ok {
				phaseMap[name] = &phaseAccum{firstSeen: len(phaseOrder)}
				phaseOrder = append(phaseOrder, name)
			}
			pa := phaseMap[name]
			pa.runCount++
			if c, ok := r.PhaseCosts[name]; ok && c > 0 {
				pa.costSum += c
				pa.costCount++
			}
			if d, ok := r.PhaseDurations[name]; ok && d > 0 {
				pa.durSum += d
				pa.durCount++
			}
			iters := r.PhaseIterations[name]
			if iters > 1 {
				pa.loopCount++
				pa.iterSum += float64(iters)
				pa.iterCount++
			}
		}
	}
	for _, name := range phaseOrder {
		pa := phaseMap[name]
		ps := PhaseStat{Name: name, RunCount: pa.runCount}
		if pa.costCount > 0 {
			ps.AvgCostUSD = pa.costSum / float64(pa.costCount)
		}
		if pa.durCount > 0 {
			ps.AvgDuration = pa.durSum / time.Duration(pa.durCount)
		}
		if pa.runCount > 0 {
			ps.LoopRate = float64(pa.loopCount) / float64(pa.runCount)
		}
		if pa.iterCount > 0 {
			ps.AvgIters = pa.iterSum / float64(pa.iterCount)
		}
		s.Phases = append(s.Phases, ps)
	}

	// 8. Failure breakdown
	failedRuns := 0
	failCounts := make(map[string]int)
	for _, r := range filtered {
		if r.Status == "failed" || r.Status == "interrupted" {
			failedRuns++
			cat := r.FailureCategory
			if cat == "" {
				cat = "unclassified"
			}
			failCounts[cat]++
		}
	}
	// Sort by count descending, then alphabetically
	type catCount struct {
		cat   string
		count int
	}
	var catCounts []catCount
	for cat, cnt := range failCounts {
		catCounts = append(catCounts, catCount{cat, cnt})
	}
	sort.Slice(catCounts, func(i, j int) bool {
		if catCounts[i].count != catCounts[j].count {
			return catCounts[i].count > catCounts[j].count
		}
		return catCounts[i].cat < catCounts[j].cat
	})
	for _, cc := range catCounts {
		pct := 0.0
		if failedRuns > 0 {
			pct = float64(cc.count) / float64(failedRuns) * 100
		}
		s.Failures = append(s.Failures, FailureStat{
			Category: cc.cat,
			Count:    cc.count,
			Percent:  pct,
		})
	}

	// 9. Weekly trend: last 5 weeks with data
	// Week key = Monday of the week
	weekCosts := make(map[time.Time][]float64)
	weekRunCount := make(map[time.Time]int)
	for _, r := range filtered {
		if r.StartTime.IsZero() {
			continue
		}
		monday := mondayOf(r.StartTime)
		weekRunCount[monday]++
		if r.CostUSD > 0 {
			weekCosts[monday] = append(weekCosts[monday], r.CostUSD)
		}
	}
	// Sort weeks descending
	var weeks []time.Time
	for w := range weekRunCount {
		weeks = append(weeks, w)
	}
	sort.Slice(weeks, func(i, j int) bool { return weeks[i].After(weeks[j]) })
	// Take last 5, then reverse to chronological order
	if len(weeks) > 5 {
		weeks = weeks[:5]
	}
	for i, j := 0, len(weeks)-1; i < j; i, j = i+1, j-1 {
		weeks[i], weeks[j] = weeks[j], weeks[i]
	}
	for _, w := range weeks {
		wCosts := weekCosts[w]
		ws := WeekStat{WeekStart: w, RunCount: weekRunCount[w]}
		if len(wCosts) > 0 {
			var sum float64
			for _, c := range wCosts {
				sum += c
			}
			ws.AvgCost = sum / float64(len(wCosts))
		}
		s.Weeks = append(s.Weeks, ws)
	}

	return s
}

// mondayOf returns the Monday of the week containing t (UTC).
func mondayOf(t time.Time) time.Time {
	t = t.UTC().Truncate(24 * time.Hour)
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday → 7
	}
	return t.AddDate(0, 0, -(weekday - 1))
}
