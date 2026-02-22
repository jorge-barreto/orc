package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// EnsureDir creates the artifacts directory structure.
func EnsureDir(artifactsDir string) error {
	dirs := []string{
		artifactsDir,
		filepath.Join(artifactsDir, "prompts"),
		filepath.Join(artifactsDir, "logs"),
		filepath.Join(artifactsDir, "feedback"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("creating artifacts dir %s: %w", d, err)
		}
	}
	return nil
}

// LoadLoopCounts reads the loop count map from artifacts.
func LoadLoopCounts(artifactsDir string) (map[string]int, error) {
	path := filepath.Join(artifactsDir, "loop-counts.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]int), nil
		}
		return nil, err
	}
	var counts map[string]int
	if err := json.Unmarshal(data, &counts); err != nil {
		return nil, err
	}
	return counts, nil
}

// SaveLoopCounts writes the loop count map to artifacts.
func SaveLoopCounts(artifactsDir string, counts map[string]int) error {
	data, err := json.MarshalIndent(counts, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(artifactsDir, "loop-counts.json"), data, 0644)
}

// WriteFeedback writes error output from a failing phase to the feedback directory.
func WriteFeedback(artifactsDir, fromPhase, content string) error {
	path := filepath.Join(artifactsDir, "feedback", fmt.Sprintf("from-%s.md", fromPhase))
	return os.WriteFile(path, []byte(content), 0644)
}

// CheckOutputs returns a list of expected output files that are missing from artifacts.
func CheckOutputs(artifactsDir string, outputs []string) []string {
	var missing []string
	for _, o := range outputs {
		path := filepath.Join(artifactsDir, o)
		if _, err := os.Stat(path); err != nil {
			missing = append(missing, o)
		}
	}
	return missing
}

// PromptPath returns the path for a rendered prompt file.
func PromptPath(artifactsDir string, idx int) string {
	return filepath.Join(artifactsDir, "prompts", fmt.Sprintf("phase-%d.md", idx+1))
}

// LogPath returns the path for a phase log file.
func LogPath(artifactsDir string, idx int) string {
	return filepath.Join(artifactsDir, "logs", fmt.Sprintf("phase-%d.log", idx+1))
}
