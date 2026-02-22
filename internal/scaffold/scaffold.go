package scaffold

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jorge-barreto/orc/internal/ux"
)

var configTemplate = `name: my-project
ticket-pattern: '[A-Z]+-\d+'

phases:
  - name: setup
    type: script
    description: Install dependencies and prepare workspace
    run: echo "Setting up workspace for $ORC_TICKET"

  - name: implement
    type: agent
    description: Implement the ticket requirements
    prompt: .orc/phases/example.md
    model: sonnet
    outputs:
      - summary.md

  - name: review
    type: gate
    description: Human review before merging
`

var promptTemplate = `You are working on ticket $TICKET.

Project root: $PROJECT_ROOT
Artifacts directory: $ARTIFACTS_DIR

## Task

Analyze the ticket requirements and produce a summary of what needs to be done.

## Output

Write a file called "summary.md" to the artifacts directory at $ARTIFACTS_DIR/summary.md
with a brief summary of the work completed.
`

// Init creates a new .orc/ directory with example config and prompt files.
func Init(targetDir string) error {
	orcDir := filepath.Join(targetDir, ".orc")
	if _, err := os.Stat(orcDir); err == nil {
		return fmt.Errorf(".orc directory already exists in %s", targetDir)
	}

	phasesDir := filepath.Join(orcDir, "phases")
	if err := os.MkdirAll(phasesDir, 0755); err != nil {
		return fmt.Errorf("creating .orc/phases: %w", err)
	}

	configPath := filepath.Join(orcDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configTemplate), 0644); err != nil {
		return fmt.Errorf("writing config.yaml: %w", err)
	}

	promptPath := filepath.Join(phasesDir, "example.md")
	if err := os.WriteFile(promptPath, []byte(promptTemplate), 0644); err != nil {
		return fmt.Errorf("writing example.md: %w", err)
	}

	fmt.Printf("\n%s%s✓ Initialized .orc/ directory%s\n\n", ux.Bold, ux.Green, ux.Reset)
	fmt.Printf("  Created:\n")
	fmt.Printf("    %s.orc/config.yaml%s    — workflow configuration\n", ux.Cyan, ux.Reset)
	fmt.Printf("    %s.orc/phases/example.md%s — example agent prompt\n\n", ux.Cyan, ux.Reset)
	fmt.Printf("  Next steps:\n")
	fmt.Printf("    1. Edit %s.orc/config.yaml%s to define your workflow\n", ux.Cyan, ux.Reset)
	fmt.Printf("    2. Add prompt files to %s.orc/phases/%s\n", ux.Cyan, ux.Reset)
	fmt.Printf("    3. Run %sorc run <ticket> --dry-run%s to preview\n\n", ux.Cyan, ux.Reset)

	return nil
}
