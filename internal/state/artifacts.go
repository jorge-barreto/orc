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

// ClearFeedback removes all files from the feedback directory.
// Returns nil if the directory does not exist.
func ClearFeedback(artifactsDir string) error {
	feedbackDir := filepath.Join(artifactsDir, "feedback")
	entries, err := os.ReadDir(feedbackDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := os.Remove(filepath.Join(feedbackDir, e.Name())); err != nil {
			return fmt.Errorf("removing feedback file %s: %w", e.Name(), err)
		}
	}
	return nil
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

// ReadDeclaredOutputs reads and concatenates the content of declared output artifact files.
// Missing or unreadable files are silently skipped. Returns empty string if no content found.
func ReadDeclaredOutputs(artifactsDir string, outputs []string) string {
	var parts []string
	for _, o := range outputs {
		data, err := os.ReadFile(filepath.Join(artifactsDir, o))
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(data))
		if content != "" {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n\n")
}

// PromptPath returns the path for a rendered prompt file.
func PromptPath(artifactsDir string, idx int) string {
	return filepath.Join(artifactsDir, "prompts", fmt.Sprintf("phase-%d.md", idx+1))
}

// LogPath returns the path for a phase log file.
func LogPath(artifactsDir string, idx int) string {
	return filepath.Join(artifactsDir, "logs", fmt.Sprintf("phase-%d.log", idx+1))
}

// StreamLogPath returns the path for a raw stream-json log file.
func StreamLogPath(artifactsDir string, idx int) string {
	return filepath.Join(artifactsDir, "logs", fmt.Sprintf("phase-%d.stream.jsonl", idx+1))
}

// AuditBaseDir returns the base audit directory for the project.
func AuditBaseDir(projectRoot string) string {
	return filepath.Join(projectRoot, ".orc", "audit")
}

// AuditDir returns the audit directory for a specific ticket.
func AuditDir(projectRoot, ticket string) string {
	return filepath.Join(projectRoot, ".orc", "audit", ticket)
}

// AuditLogPath returns the path for an archived iteration log in the audit dir.
func AuditLogPath(auditDir string, phaseIdx, iteration int) string {
	return filepath.Join(auditDir, "logs", fmt.Sprintf("phase-%d.iter-%d.log", phaseIdx+1, iteration))
}

// AuditPromptPath returns the path for an archived iteration prompt in the audit dir.
func AuditPromptPath(auditDir string, phaseIdx, iteration int) string {
	return filepath.Join(auditDir, "prompts", fmt.Sprintf("phase-%d.iter-%d.md", phaseIdx+1, iteration))
}

// AuditFeedbackPath returns the path for an archived feedback file in the audit dir.
func AuditFeedbackPath(auditDir string, phaseIdx, iteration int, fromPhase string) string {
	return filepath.Join(auditDir, "feedback", fmt.Sprintf("phase-%d.iter-%d.from-%s.md", phaseIdx+1, iteration, fromPhase))
}
