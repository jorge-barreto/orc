package stats

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/jorge-barreto/orc/internal/state"
	"github.com/jorge-barreto/orc/internal/ux"
)

// RenderText writes a human-readable stats report to w.
func RenderText(w io.Writer, s *Stats) {
	fmt.Fprintf(w, "\n  %sorc stats%s — %d runs across %d tickets (%s)\n\n", ux.Bold, ux.Reset, s.TotalRuns, s.TotalTickets, s.DateRange)
	if s.TotalRuns == 0 {
		fmt.Fprintf(w, "  No audited runs found.\n")
		return
	}

	// Overall section
	fmt.Fprintf(w, "  %sOverall%s\n", ux.Bold, ux.Reset)
	fmt.Fprintf(w, "    Success rate:  %d%%  (%d/%d)\n", int(math.Round(s.SuccessRate)), s.SuccessCount, s.TotalRuns)

	if s.AvgCostUSD == 0 {
		fmt.Fprintf(w, "    Avg cost:      —\n")
	} else if s.P95CostUSD > 0 {
		fmt.Fprintf(w, "    Avg cost:      $%.2f  (p95: $%.2f)\n", s.AvgCostUSD, s.P95CostUSD)
	} else {
		fmt.Fprintf(w, "    Avg cost:      $%.2f\n", s.AvgCostUSD)
	}

	if s.AvgDuration == 0 {
		fmt.Fprintf(w, "    Avg duration:  —\n")
	} else if s.P95Duration > 0 {
		fmt.Fprintf(w, "    Avg duration:  %s  (p95: %s)\n", state.FormatDuration(s.AvgDuration), state.FormatDuration(s.P95Duration))
	} else {
		fmt.Fprintf(w, "    Avg duration:  %s\n", state.FormatDuration(s.AvgDuration))
	}
	fmt.Fprintln(w)

	// Phase Breakdown section
	if len(s.Phases) > 0 {
		fmt.Fprintf(w, "  %sPhase Breakdown%s\n", ux.Bold, ux.Reset)
		nameWidth := 5 // min width for "PHASE"
		for _, ps := range s.Phases {
			if len(ps.Name) > nameWidth {
				nameWidth = len(ps.Name)
			}
		}
		fmt.Fprintf(w, "    %s%-*s   %-9s  %-9s  %-10s  %-9s%s\n",
			ux.Dim, nameWidth, "PHASE", "AVG COST", "AVG TIME", "LOOP RATE", "AVG ITERS", ux.Reset)
		for _, ps := range s.Phases {
			cost := "—"
			if ps.AvgCostUSD > 0 {
				cost = fmt.Sprintf("$%.2f", ps.AvgCostUSD)
			}
			dur := "—"
			if ps.AvgDuration > 0 {
				dur = state.FormatDuration(ps.AvgDuration)
			}
			loopRate := "—"
			if ps.LoopRate > 0 {
				loopRate = fmt.Sprintf("%.0f%%", ps.LoopRate*100)
			}
			avgIters := "—"
			if ps.AvgIters > 0 {
				avgIters = fmt.Sprintf("%.1f", ps.AvgIters)
			}
			fmt.Fprintf(w, "    %-*s   %-9s  %-9s  %-10s  %-9s\n",
				nameWidth, ps.Name, cost, dur, loopRate, avgIters)
		}
		fmt.Fprintln(w)
	}

	// Failure Breakdown section
	if len(s.Failures) > 0 {
		fmt.Fprintf(w, "  %sFailure Breakdown%s\n", ux.Bold, ux.Reset)
		catWidth := 0
		for _, f := range s.Failures {
			cat := strings.ReplaceAll(f.Category, "_", " ")
			cat = strings.ToUpper(cat[:1]) + cat[1:]
			if len(cat) > catWidth {
				catWidth = len(cat)
			}
		}
		catWidth++ // pad to longest+1
		for _, f := range s.Failures {
			cat := strings.ReplaceAll(f.Category, "_", " ")
			cat = strings.ToUpper(cat[:1]) + cat[1:]
			fmt.Fprintf(w, "    %-*s  %d  (%.1f%%)\n", catWidth, cat, f.Count, f.Percent)
		}
		fmt.Fprintln(w)
	}

	// Cost Trend section
	if len(s.Weeks) > 0 {
		fmt.Fprintf(w, "  %sCost Trend (last 5 weeks)%s\n", ux.Bold, ux.Reset)
		for _, ws := range s.Weeks {
			label := ws.WeekStart.Format("Jan 02")
			if ws.AvgCost == 0 && ws.RunCount > 0 {
				fmt.Fprintf(w, "    %s:  — avg  (%d runs)\n", label, ws.RunCount)
			} else {
				fmt.Fprintf(w, "    %s:  $%.2f avg  (%d runs)\n", label, ws.AvgCost, ws.RunCount)
			}
		}
		fmt.Fprintln(w)
	}
}

// JSON wrapper structs for duration-as-string serialization.

type jsonStats struct {
	TotalRuns    int           `json:"total_runs"`
	TotalTickets int           `json:"total_tickets"`
	DateRange    string        `json:"date_range"`
	SuccessCount int           `json:"success_count"`
	SuccessRate  float64       `json:"success_rate"`
	AvgCostUSD   float64       `json:"avg_cost_usd"`
	P95CostUSD   float64       `json:"p95_cost_usd"`
	AvgDuration  string        `json:"avg_duration"`
	P95Duration  string        `json:"p95_duration"`
	Phases       []jsonPhase   `json:"phases"`
	Failures     []FailureStat `json:"failures"`
	Weeks        []jsonWeek    `json:"weeks"`
}

type jsonPhase struct {
	Name        string  `json:"name"`
	AvgCostUSD  float64 `json:"avg_cost_usd"`
	AvgDuration string  `json:"avg_duration"`
	LoopRate    float64 `json:"loop_rate"`
	AvgIters    float64 `json:"avg_iters"`
	RunCount    int     `json:"run_count"`
}

type jsonWeek struct {
	WeekStart string  `json:"week_start"`
	AvgCost   float64 `json:"avg_cost"`
	RunCount  int     `json:"run_count"`
}

func toJSONStats(s *Stats) jsonStats {
	js := jsonStats{
		TotalRuns:    s.TotalRuns,
		TotalTickets: s.TotalTickets,
		DateRange:    s.DateRange,
		SuccessCount: s.SuccessCount,
		SuccessRate:  s.SuccessRate,
		AvgCostUSD:   s.AvgCostUSD,
		P95CostUSD:   s.P95CostUSD,
		AvgDuration:  state.FormatDuration(s.AvgDuration),
		P95Duration:  state.FormatDuration(s.P95Duration),
		Failures:     s.Failures,
	}
	for _, p := range s.Phases {
		js.Phases = append(js.Phases, jsonPhase{
			Name:        p.Name,
			AvgCostUSD:  p.AvgCostUSD,
			AvgDuration: state.FormatDuration(p.AvgDuration),
			LoopRate:    p.LoopRate,
			AvgIters:    p.AvgIters,
			RunCount:    p.RunCount,
		})
	}
	for _, w := range s.Weeks {
		js.Weeks = append(js.Weeks, jsonWeek{
			WeekStart: w.WeekStart.Format("Jan 02"),
			AvgCost:   w.AvgCost,
			RunCount:  w.RunCount,
		})
	}
	return js
}

// RenderJSON writes s as indented JSON to w.
func RenderJSON(w io.Writer, s *Stats) error {
	data, err := json.MarshalIndent(toJSONStats(s), "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}
