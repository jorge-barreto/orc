package scaffold

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/contextgather"
	"github.com/jorge-barreto/orc/internal/dispatch"
	"github.com/jorge-barreto/orc/internal/fileblocks"
	"github.com/jorge-barreto/orc/internal/ux"
)

// Init creates a new .orc/ directory with AI-generated workflow config and prompt files.
func Init(ctx context.Context, targetDir, userPrompt string) error {
	orcDir := filepath.Join(targetDir, ".orc")
	if _, err := os.Stat(orcDir); err == nil {
		return fmt.Errorf(".orc directory already exists in %s", targetDir)
	}

	return initWithAI(ctx, targetDir, userPrompt)
}

// initWithAI gathers project context, calls claude with retries, and writes AI-generated files.
// Falls back to a default template if all attempts fail.
func initWithAI(ctx context.Context, targetDir, userPrompt string) error {
	fmt.Printf("\n  %sAnalyzing project...%s\n", ux.Dim, ux.Reset)

	pc, err := contextgather.Gather(targetDir)
	if err != nil {
		return fmt.Errorf("gathering context: %w", err)
	}

	prompt := buildInitPrompt(pc.Render(), userPrompt)

	const maxAttempts = 3
	var blocks []fileblocks.FileBlock
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt == 1 {
			fmt.Printf("  %sGenerating workflow config...%s\n", ux.Dim, ux.Reset)
		} else {
			fmt.Printf("  %s↺ Retrying (%d/%d): %v%s\n", ux.Yellow, attempt, maxAttempts, lastErr, ux.Reset)
		}

		currentPrompt := prompt
		if attempt > 1 {
			currentPrompt = prompt + fmt.Sprintf(retryFeedback, lastErr)
		}

		blocks, lastErr = generateConfig(ctx, currentPrompt)
		if lastErr == nil {
			break
		}
	}

	if lastErr != nil {
		fmt.Printf("\n  %s⚠ AI generation failed after %d attempts: %v%s\n",
			ux.Yellow, maxAttempts, lastErr, ux.Reset)
		fmt.Printf("  %sUsing default workflow template...%s\n", ux.Dim, ux.Reset)
		return writeFallbackConfig(targetDir)
	}

	// Write validated files to the target directory
	written := writeBlocks(targetDir, blocks)

	// Write .gitignore (deterministic, not AI-generated)
	gitignorePath := filepath.Join(targetDir, ".orc", ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("artifacts/\n"), 0644); err != nil {
		return fmt.Errorf("writing .orc/.gitignore: %w", err)
	}
	written = append(written, ".orc/.gitignore")

	printSuccess("AI-generated", written)

	// Config is already validated by generateConfig; load for workflow summary
	configPath := filepath.Join(targetDir, ".orc", "config.yaml")
	if cfg, err := config.Load(configPath, targetDir); err == nil {
		fmt.Printf("\n  Workflow: %s%s%s\n", ux.Bold, renderWorkflowSummary(cfg.Phases), ux.Reset)
	}

	fmt.Printf("\n  Next: %sorc run <ticket> --dry-run%s\n\n", ux.Cyan, ux.Reset)
	return nil
}

// generateConfig calls claude, parses the output, and validates the generated config
// in a temp directory. Returns the validated file blocks or an error.
func generateConfig(ctx context.Context, prompt string) ([]fileblocks.FileBlock, error) {
	output, err := runClaude(ctx, prompt)
	if err != nil {
		return nil, err
	}

	blocks := fileblocks.Parse(output)
	if len(blocks) == 0 {
		return nil, fmt.Errorf("no file blocks in output")
	}

	hasConfig := false
	for _, b := range blocks {
		if b.Path == ".orc/config.yaml" {
			hasConfig = true
		}
	}
	if !hasConfig {
		return nil, fmt.Errorf("output missing .orc/config.yaml")
	}

	// Write to temp dir and validate
	tmpDir, err := os.MkdirTemp("", "orc-init-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	for _, b := range blocks {
		if !strings.HasPrefix(b.Path, ".orc/") {
			continue
		}
		fullPath := filepath.Join(tmpDir, b.Path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return nil, fmt.Errorf("creating temp dir for %s: %w", b.Path, err)
		}
		if err := os.WriteFile(fullPath, []byte(b.Content), 0644); err != nil {
			return nil, fmt.Errorf("writing temp %s: %w", b.Path, err)
		}
	}

	if _, err := config.Load(filepath.Join(tmpDir, ".orc", "config.yaml"), tmpDir); err != nil {
		return nil, fmt.Errorf("generated config is invalid: %w", err)
	}

	return blocks, nil
}

// writeBlocks writes validated file blocks to the target directory.
func writeBlocks(targetDir string, blocks []fileblocks.FileBlock) []string {
	var written []string
	for _, b := range blocks {
		if !strings.HasPrefix(b.Path, ".orc/") {
			continue
		}
		fullPath := filepath.Join(targetDir, b.Path)
		os.MkdirAll(filepath.Dir(fullPath), 0755)
		os.WriteFile(fullPath, []byte(b.Content), 0644)
		written = append(written, b.Path)
	}
	return written
}

// printSuccess prints the initialization success message and file list.
func printSuccess(source string, written []string) {
	fmt.Printf("\n%s%s  ✓ Initialized .orc/ directory (%s)%s\n\n", ux.Bold, ux.Green, source, ux.Reset)
	fmt.Printf("  Created:\n")
	for _, path := range written {
		fmt.Printf("    %s%s%s\n", ux.Cyan, path, ux.Reset)
	}
}

// runClaude is the function used to invoke claude. Tests can override this.
var runClaude = runClaudeCaptureDefault

// runClaudeCaptureDefault writes the prompt to a temp file and invokes claude -p
// with a short instruction to read it. This avoids ARG_MAX limits when the prompt
// (schema + examples + project context) exceeds the OS command-line size limit.
func runClaudeCaptureDefault(ctx context.Context, prompt string) (string, error) {
	f, err := os.CreateTemp("", "orc-init-prompt-*.md")
	if err != nil {
		return "", fmt.Errorf("creating prompt file: %w", err)
	}
	promptFile := f.Name()
	defer os.Remove(promptFile)

	if _, err := f.WriteString(prompt); err != nil {
		f.Close()
		return "", fmt.Errorf("writing prompt file: %w", err)
	}
	f.Close()

	cmd := exec.CommandContext(ctx, "claude", "-p",
		"Read the file at "+promptFile+" and follow its instructions exactly.",
		"--model", "opus", "--effort", "high")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	cmd.Env = dispatch.FilteredEnv()
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude: %w", err)
	}
	return stdout.String(), nil
}

// renderWorkflowSummary builds a human-readable workflow line.
// Sequential phases are joined with →, parallel phases with ∥.
func renderWorkflowSummary(phases []config.Phase) string {
	// Build map of parallel partners: the "earlier" phase -> "earlier ∥ later"
	parallelOf := make(map[string]string)
	skipSelf := make(map[string]bool)
	for _, p := range phases {
		if p.ParallelWith != "" {
			parallelOf[p.ParallelWith] = fmt.Sprintf("%s ∥ %s", p.ParallelWith, p.Name)
			skipSelf[p.Name] = true
		}
	}

	var parts []string
	for _, p := range phases {
		if skipSelf[p.Name] {
			continue
		}
		if group, ok := parallelOf[p.Name]; ok {
			parts = append(parts, group)
		} else {
			parts = append(parts, p.Name)
		}
	}
	return strings.Join(parts, " → ")
}
