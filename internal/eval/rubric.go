package eval

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// CriterionResult holds the outcome of evaluating one rubric criterion.
type CriterionResult struct {
	Name   string
	Score  float64 // 0.0–1.0
	Pass   bool
	Detail string
}

// CaseResult holds the aggregate outcome of one eval case.
type CaseResult struct {
	Name            string             `json:"name"`
	Score           int                `json:"score"`
	CostUSD         float64            `json:"cost_usd"`
	DurationSeconds float64            `json:"duration_seconds"`
	PassCount       int                `json:"pass_count"`
	TotalCount      int                `json:"total_count"`
	Failures        []string           `json:"failures"`
	Details         map[string]float64 `json:"details"`
	WorkflowErr     string             `json:"workflow_err,omitempty"`
}

var scoreRegex = regexp.MustCompile(`SCORE:\s*(\d+)`)

func filteredEnv(extras ...string) []string {
	overridden := map[string]bool{
		"ARTIFACTS_DIR": true, "WORK_DIR": true, "PROJECT_ROOT": true,
	}
	var env []string
	for _, e := range os.Environ() {
		idx := strings.IndexByte(e, '=')
		if idx < 0 {
			continue
		}
		key := e[:idx]
		if strings.HasPrefix(key, "CLAUDECODE") || strings.HasPrefix(key, "ORC_") || overridden[key] {
			continue
		}
		env = append(env, e)
	}
	return append(env, extras...)
}

// EvaluateRubric runs all criteria in the rubric and returns results.
func EvaluateRubric(ctx context.Context, rubric *Rubric, artifactsDir, worktreePath, projectRoot string) ([]CriterionResult, error) {
	var results []CriterionResult
	for _, c := range rubric.Criteria {
		if ctx.Err() != nil {
			break
		}

		if c.Check != "" {
			cmd := exec.CommandContext(ctx, "bash", "-c", c.Check)
			cmd.Dir = worktreePath
			cmd.Env = filteredEnv("ARTIFACTS_DIR="+artifactsDir, "WORK_DIR="+worktreePath, "PROJECT_ROOT="+projectRoot)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
			cmd.WaitDelay = 5 * time.Second
			err := cmd.Run()
			exitCode := 0
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					exitCode = exitErr.ExitCode()
				} else {
					exitCode = 1
				}
			}
			pass := parseExpect(c.Expect, exitCode, 0, false)
			score := 0.0
			if pass {
				score = 1.0
			}
			results = append(results, CriterionResult{
				Name:   c.Name,
				Score:  score,
				Pass:   pass,
				Detail: fmt.Sprintf("exit %d", exitCode),
			})
		} else if c.Judge {
			promptData, err := os.ReadFile(filepath.Join(projectRoot, c.Prompt))
			if err != nil {
				results = append(results, CriterionResult{
					Name:   c.Name,
					Score:  0,
					Pass:   false,
					Detail: "failed to read prompt: " + err.Error(),
				})
				continue
			}
			cmd := exec.CommandContext(ctx, "claude", "-p", "--model", "sonnet", "--output-format", "text")
			cmd.Dir = worktreePath
			cmd.Env = filteredEnv("ARTIFACTS_DIR="+artifactsDir, "WORK_DIR="+worktreePath, "PROJECT_ROOT="+projectRoot)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
			cmd.WaitDelay = 5 * time.Second
			cmd.Stdin = bytes.NewReader(promptData)
			out, err := cmd.Output()
			if err != nil {
				results = append(results, CriterionResult{
					Name:   c.Name,
					Score:  0,
					Pass:   false,
					Detail: "claude error: " + err.Error(),
				})
				continue
			}

			matches := scoreRegex.FindAllStringSubmatch(string(out), -1)
			if len(matches) == 0 {
				results = append(results, CriterionResult{Name: c.Name, Score: 0, Pass: false, Detail: "no SCORE found"})
				continue
			}
			last := matches[len(matches)-1]
			n, _ := strconv.Atoi(last[1])
			judgeScore := float64(n)
			normalizedScore := math.Min(math.Max(judgeScore/10.0, 0.0), 1.0)
			pass := parseExpect(c.Expect, 0, judgeScore, true)
			results = append(results, CriterionResult{
				Name:   c.Name,
				Score:  normalizedScore,
				Pass:   pass,
				Detail: fmt.Sprintf("SCORE: %d", n),
			})
		}
	}
	if ctx.Err() != nil {
		return results, ctx.Err()
	}
	return results, nil
}

// ComputeScore returns a 0–100 weighted score from criterion results.
func ComputeScore(results []CriterionResult, rubric *Rubric) int {
	weights := make(map[string]float64)
	for _, c := range rubric.Criteria {
		weights[c.Name] = c.Weight
	}

	var weightedSum, totalWeight float64
	for _, r := range results {
		w := weights[r.Name]
		weightedSum += r.Score * w
		totalWeight += w
	}

	if totalWeight == 0 {
		return 0
	}
	result := int(math.Round(weightedSum / totalWeight * 100))
	if result > 100 {
		result = 100
	}
	if result < 0 {
		result = 0
	}
	return result
}

// parseExpect interprets the expect field and returns pass/fail.
// For script criteria (isJudge=false): parses "exit N", exitCode must equal N.
// For judge criteria (isJudge=true): parses ">= N", "> N", "<= N", "< N", "== N".
func parseExpect(expect string, exitCode int, judgeScore float64, isJudge bool) bool {
	if !isJudge {
		if !strings.HasPrefix(expect, "exit ") {
			return exitCode == 0
		}
		n, err := strconv.Atoi(strings.TrimPrefix(expect, "exit "))
		if err != nil {
			return exitCode == 0
		}
		return exitCode == n
	}

	parts := strings.SplitN(strings.TrimSpace(expect), " ", 2)
	if len(parts) != 2 {
		return judgeScore >= 7
	}
	op := parts[0]
	val, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return judgeScore >= 7
	}
	switch op {
	case ">=":
		return judgeScore >= val
	case ">":
		return judgeScore > val
	case "<=":
		return judgeScore <= val
	case "<":
		return judgeScore < val
	case "==":
		return judgeScore == val
	default:
		return judgeScore >= 7
	}
}

// isValidJudgeExpect reports whether expect is a valid judge criterion expect string.
// Valid form: "<op> <float>" where op is one of >=, >, <=, <, ==.
func isValidJudgeExpect(expect string) bool {
	parts := strings.SplitN(strings.TrimSpace(expect), " ", 2)
	if len(parts) != 2 {
		return false
	}
	switch parts[0] {
	case ">=", ">", "<=", "<", "==":
	default:
		return false
	}
	_, err := strconv.ParseFloat(parts[1], 64)
	return err == nil
}

// isValidScriptExpect reports whether expect is a valid script criterion expect string.
// Valid form: "exit <int>".
func isValidScriptExpect(expect string) bool {
	if !strings.HasPrefix(expect, "exit ") {
		return false
	}
	_, err := strconv.Atoi(strings.TrimPrefix(expect, "exit "))
	return err == nil
}
