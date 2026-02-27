package scaffold

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/ux"
)

const fallbackConfig = `name: my-project

phases:
  - name: plan
    type: agent
    description: Analyze the ticket and produce an implementation plan
    prompt: .orc/phases/plan.md
    outputs:
      - plan.md

  - name: implement
    type: agent
    description: Implement the changes following the plan
    prompt: .orc/phases/implement.md

  - name: review
    type: gate
    description: Human reviews the implementation
    on-fail:
      goto: implement
      max: 3
`

const fallbackPlanPrompt = `You are a planning agent working on ticket $TICKET.

## Instructions

1. Read and understand the ticket requirements.
2. Explore the codebase under $PROJECT_ROOT to understand the relevant code.
3. Write a clear implementation plan to $ARTIFACTS_DIR/plan.md.

The plan should include:
- Summary of the changes needed
- Files to modify or create
- Key implementation details
- Testing approach
`

const fallbackImplementPrompt = `You are an implementation agent working on ticket $TICKET.

## Instructions

1. Read the plan at $ARTIFACTS_DIR/plan.md.
2. Implement the changes described in the plan.
3. Follow existing code conventions in the project.
4. Run any relevant tests to verify your changes.

Work in $PROJECT_ROOT.
`

// writeFallbackConfig writes a minimal default workflow when AI generation fails.
func writeFallbackConfig(targetDir string) error {
	files := map[string]string{
		".orc/config.yaml":          fallbackConfig,
		".orc/phases/plan.md":       fallbackPlanPrompt,
		".orc/phases/implement.md":  fallbackImplementPrompt,
	}

	var written []string
	for relPath, content := range files {
		fullPath := filepath.Join(targetDir, relPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return fmt.Errorf("creating directory for %s: %w", relPath, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", relPath, err)
		}
		written = append(written, relPath)
	}

	// Write .gitignore
	gitignorePath := filepath.Join(targetDir, ".orc", ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("artifacts/\n"), 0644); err != nil {
		return fmt.Errorf("writing .orc/.gitignore: %w", err)
	}
	written = append(written, ".orc/.gitignore")

	printSuccess("default template", written)

	// Load and print workflow summary
	configPath := filepath.Join(targetDir, ".orc", "config.yaml")
	if cfg, err := config.Load(configPath, targetDir); err == nil {
		fmt.Printf("\n  Workflow: %s%s%s\n", ux.Bold, renderWorkflowSummary(cfg.Phases), ux.Reset)
	}

	fmt.Printf("\n  %sCustomize .orc/config.yaml and phase prompts for your project.%s\n", ux.Dim, ux.Reset)
	fmt.Printf("\n  Next: %sorc run <ticket> --dry-run%s\n\n", ux.Cyan, ux.Reset)
	return nil
}
