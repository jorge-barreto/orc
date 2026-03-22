package eval

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/jorge-barreto/orc/internal/state"
	"github.com/jorge-barreto/orc/internal/ux"
)

// RenderScoreReport prints a formatted table of eval case results.
func RenderScoreReport(w io.Writer, fingerprint string, cases []CaseResult) {
	fmt.Fprintf(w, "\n  %sorc eval%s — %d cases, config fingerprint %s\n\n",
		ux.Bold, ux.Reset, len(cases), fingerprint)

	fmt.Fprintf(w, "  %s%-16s %-8s %-9s %-10s %s%s\n",
		ux.Dim, "CASE", "SCORE", "COST", "TIME", "PASS/FAIL", ux.Reset)

	for _, c := range cases {
		scoreColor := ux.Red
		if c.Score >= 80 {
			scoreColor = ux.Green
		} else if c.Score >= 60 {
			scoreColor = ux.Yellow
		}

		passFail := fmt.Sprintf("%d/%d pass", c.PassCount, c.TotalCount)
		if len(c.Failures) > 0 {
			passFail += " (" + strings.Join(c.Failures, ", ") + ")"
		}

		dur := time.Duration(float64(time.Second) * c.DurationSeconds)
		fmt.Fprintf(w, "  %-16s %s%d/100%s   $%-7.2f %-10s %s\n",
			c.Name,
			scoreColor, c.Score, ux.Reset,
			c.CostUSD,
			state.FormatDuration(dur),
			passFail,
		)
	}

	var totalScore, totalCost, totalDur float64
	for _, c := range cases {
		totalScore += float64(c.Score)
		totalCost += c.CostUSD
		totalDur += c.DurationSeconds
	}
	avgScore := 0.0
	if len(cases) > 0 {
		avgScore = totalScore / float64(len(cases))
	}
	totalDuration := time.Duration(float64(time.Second) * totalDur)
	fmt.Fprintf(w, "\n  Totals: %d/100 avg, $%.2f total cost, %s total time\n",
		int(math.Round(avgScore)),
		totalCost,
		state.FormatDuration(totalDuration),
	)
}

// RenderHistoryReport prints a formatted table of historical eval runs.
func RenderHistoryReport(w io.Writer, history *History) {
	fmt.Fprintf(w, "\n  %sorc eval --report%s — score history\n\n", ux.Bold, ux.Reset)
	fmt.Fprintf(w, "  %s%-12s %-13s %-10s %-11s %s%s\n",
		ux.Dim, "FINGERPRINT", "DATE", "AVG SCORE", "TOTAL COST", "TOTAL TIME", ux.Reset)

	for _, entry := range history.Runs {
		t, err := time.Parse(time.RFC3339, entry.Timestamp)
		dateStr := entry.Timestamp
		if err == nil {
			dateStr = t.Format("Jan 02 15:04")
		}

		var totalScore, totalCost, totalDur float64
		for _, c := range entry.Cases {
			totalScore += float64(c.Score)
			totalCost += c.CostUSD
			totalDur += c.DurationSeconds
		}
		avgScore := 0.0
		if len(entry.Cases) > 0 {
			avgScore = totalScore / float64(len(entry.Cases))
		}
		totalDuration := time.Duration(float64(time.Second) * totalDur)
		fmt.Fprintf(w, "  %-12s %-13s %d/100      $%-10.2f %s\n",
			entry.ConfigFingerprint,
			dateStr,
			int(math.Round(avgScore)),
			totalCost,
			state.FormatDuration(totalDuration),
		)
	}
}

// RenderJSON writes eval results as JSON to w.
func RenderJSON(w io.Writer, fingerprint string, cases []CaseResult) error {
	out := struct {
		Fingerprint string       `json:"fingerprint"`
		Cases       []CaseResult `json:"cases"`
	}{
		Fingerprint: fingerprint,
		Cases:       cases,
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("eval: marshaling JSON: %w", err)
	}
	_, err = w.Write(data)
	return err
}

// RenderCaseList lists eval cases with descriptions to w.
func RenderCaseList(w io.Writer, projectRoot string) error {
	cases, err := DiscoverCases(projectRoot)
	if err != nil {
		return err
	}
	for _, name := range cases {
		caseDir := filepath.Join(projectRoot, ".orc", "evals", name)
		fixture, err := LoadFixture(caseDir)
		desc := ""
		if err == nil && fixture.Description != "" {
			desc = "  " + fixture.Description
		}
		fmt.Fprintf(w, "  %s%s\n", name, desc)
	}
	return nil
}
