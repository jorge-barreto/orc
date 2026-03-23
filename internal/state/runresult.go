package state

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
)

// RunResult holds the outcome of a completed workflow run.
type RunResult struct {
	Ticket               string   `json:"ticket"`
	Workflow             string   `json:"workflow"`
	Status               string   `json:"status"`
	ExitCode             int      `json:"exit_code"`
	FailedPhase          *string  `json:"failed_phase"`
	PhasesCompleted      int      `json:"phases_completed"`
	PhasesTotal          int      `json:"phases_total"`
	TotalCostUSD         float64  `json:"total_cost_usd"`
	TotalDurationSeconds float64  `json:"total_duration_seconds"`
	Commits              []string `json:"commits"`
	ArtifactsDir         string   `json:"artifacts_dir"`
}

// RunResultPath returns the path to the run-result.json file.
func RunResultPath(artifactsDir string) string {
	return filepath.Join(artifactsDir, "run-result.json")
}

// WriteRunResult writes the run result to disk atomically.
func WriteRunResult(artifactsDir string, result *RunResult) error {
	if result.Commits == nil {
		result.Commits = []string{}
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(RunResultPath(artifactsDir), data, 0644)
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
