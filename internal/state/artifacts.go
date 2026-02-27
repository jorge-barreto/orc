package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
		if errors.Is(err, fs.ErrNotExist) {
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
	return writeFileAtomic(filepath.Join(artifactsDir, "loop-counts.json"), data, 0644)
}

// WriteFeedback writes error output from a failing phase to the feedback directory.
func WriteFeedback(artifactsDir, fromPhase, content string) error {
	path := filepath.Join(artifactsDir, "feedback", fmt.Sprintf("from-%s.md", fromPhase))
	return writeFileAtomic(path, []byte(content), 0644)
}

// ReadAllFeedback reads all feedback files and returns them as a formatted string.
// Returns empty string if no feedback exists.
func ReadAllFeedback(artifactsDir string) (string, error) {
	feedbackDir := filepath.Join(artifactsDir, "feedback")
	entries, err := os.ReadDir(feedbackDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}

	var parts []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(feedbackDir, e.Name()))
		if err != nil {
			return "", err
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}
		// Extract phase name from filename "from-<phase>.md"
		name := e.Name()
		name = strings.TrimPrefix(name, "from-")
		name = strings.TrimSuffix(name, ".md")
		parts = append(parts, fmt.Sprintf("--- Feedback from %s ---\n%s", name, content))
	}
	if len(parts) == 0 {
		return "", nil
	}
	return strings.Join(parts, "\n\n"), nil
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
