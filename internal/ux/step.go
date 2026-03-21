package ux

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jorge-barreto/orc/internal/state"
)

// StepAction represents the user's choice at a step-through prompt.
type StepAction struct {
	Type   string // "continue", "rewind", "abort", "inspect", "unknown"
	Target string // phase ref for rewind, filename for inspect
}

// ParseStepInput parses a single line of user input into a StepAction.
func ParseStepInput(input string) StepAction {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return StepAction{Type: "continue"}
	}
	switch {
	case strings.EqualFold(fields[0], "c"), strings.EqualFold(fields[0], "continue"):
		return StepAction{Type: "continue"}
	case strings.EqualFold(fields[0], "a"), strings.EqualFold(fields[0], "abort"):
		return StepAction{Type: "abort"}
	case strings.EqualFold(fields[0], "r"), strings.EqualFold(fields[0], "rewind"):
		if len(fields) < 2 {
			return StepAction{Type: "unknown"}
		}
		return StepAction{Type: "rewind", Target: fields[1]}
	case strings.EqualFold(fields[0], "i"), strings.EqualFold(fields[0], "inspect"):
		if len(fields) < 2 {
			return StepAction{Type: "unknown"}
		}
		return StepAction{Type: "inspect", Target: fields[1]}
	default:
		return StepAction{Type: "unknown"}
	}
}

// safeArtifactPath validates that target resolves within artifactsDir.
func safeArtifactPath(artifactsDir, target string) (string, error) {
	clean := filepath.Clean(target)
	full := filepath.Join(artifactsDir, clean)
	if !strings.HasPrefix(full, artifactsDir+string(filepath.Separator)) && full != artifactsDir {
		return "", fmt.Errorf("path %q escapes artifacts directory", target)
	}
	return full, nil
}

// listStepArtifacts returns regular files in artifactsDir plus the phase log if present.
func listStepArtifacts(artifactsDir string, phaseIdx int) []string {
	entries, err := os.ReadDir(artifactsDir)
	var result []string
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				result = append(result, e.Name())
			}
		}
	}
	logPath := state.LogPath(artifactsDir, phaseIdx)
	if _, err := os.Stat(logPath); err == nil {
		rel, _ := filepath.Rel(artifactsDir, logPath)
		result = append(result, rel)
	}
	sort.Strings(result)
	return result
}

// StepPrompt displays artifacts and prompts the user for a step-through action.
func StepPrompt(artifactsDir string, phaseIdx int, phaseName string) StepAction {
	files := listStepArtifacts(artifactsDir, phaseIdx)
	if len(files) > 0 {
		fmt.Print("\n  Artifacts written:\n")
		for _, f := range files {
			info, err := os.Stat(filepath.Join(artifactsDir, f))
			size := "unknown"
			if err == nil {
				b := info.Size()
				if b >= 1024 {
					size = fmt.Sprintf("%.1f KB", float64(b)/1024.0)
				} else {
					size = fmt.Sprintf("%d bytes", b)
				}
			}
			fmt.Printf("    %s (%s)\n", f, size)
		}
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("\n  [c]ontinue  [r]ewind to phase  [a]bort  [i]nspect artifact > ")
		line, _ := reader.ReadString('\n')
		action := ParseStepInput(strings.TrimSpace(line))
		switch action.Type {
		case "inspect":
			full, err := safeArtifactPath(artifactsDir, action.Target)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  error: %v\n", err)
				continue
			}
			data, err := os.ReadFile(full)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  error reading file: %v\n", err)
				continue
			}
			fmt.Printf("\n%s\n", data)
		case "unknown":
			fmt.Print("  hint: c, r <phase>, a, i <file>\n")
		default:
			return action
		}
	}
}
