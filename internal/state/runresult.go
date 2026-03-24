package state

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
)

// PhaseResult holds per-phase outcome data for run-result.json.
type PhaseResult struct {
	Name            string  `json:"name"`
	Status          string  `json:"status"`
	DurationSeconds float64 `json:"duration_seconds"`
	CostUSD         float64 `json:"cost_usd"`
}

// RunResult holds the outcome of a completed workflow run.
type RunResult struct {
	Ticket               string        `json:"ticket"`
	Workflow             string        `json:"workflow"`
	Status               string        `json:"status"`
	ExitCode             int           `json:"exit_code"`
	FailedPhase          *string       `json:"failed_phase"`
	PhasesCompleted      int           `json:"phases_completed"`
	PhasesTotal          int           `json:"phases_total"`
	TotalCostUSD         float64       `json:"total_cost_usd"`
	TotalDurationSeconds float64       `json:"total_duration_seconds"`
	Commits              []string      `json:"commits"`
	ArtifactsDir         string        `json:"artifacts_dir"`
	Phases               []PhaseResult `json:"phases"`
}

// RunResultPath returns the path to the run-result.json file.
func RunResultPath(dir string) string {
	return filepath.Join(dir, "run-result.json")
}

// WriteRunResult writes the run result to disk atomically. It does not modify the passed RunResult.
func WriteRunResult(dir string, result *RunResult) error {
	local := *result
	if local.Commits == nil {
		local.Commits = []string{}
	}
	if local.Phases == nil {
		local.Phases = []PhaseResult{}
	}
	data, err := json.MarshalIndent(local, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(RunResultPath(dir), data, 0644)
}

// CollectCommits returns the list of commit hashes between baseCommit and HEAD.
// Returns nil if baseCommit is empty, git is unavailable, or an error occurs.
func CollectCommits(projectRoot, baseCommit string) []string {
	if baseCommit == "" {
		return nil
	}
	cmd := exec.Command("git", "log", "--oneline", "--format=%h", baseCommit+"..HEAD")
	cmd.Dir = projectRoot
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var commits []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			commits = append(commits, line)
		}
	}
	return commits
}
